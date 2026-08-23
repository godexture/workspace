package audio

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/side"
	"github.com/godexture/godec/media/timing"
)

// resamplePlan is what Compile settled for a stage that changes how many
// samples there are: the two rates it interpolates between, and the rate the
// result is counted in.
type resamplePlan struct {
	shape      flow.Shape
	inputRate  int
	targetRate int
	outputRate int
	channels   int
	samples    int
	detail     string
}

// resampleOperator emits as many samples as the interpolation produced, which
// is not one frame's worth and is sometimes none at all. Timestamps run on the
// operator's own count rather than the input's, because the positions it emits
// no longer line up with the ones it read.
type resampleOperator struct {
	shape    flow.Shape
	buffers  *buffer.Allocator
	kernel   *resampler
	channels int
	maximum  int
	rates    [2]int
	planes   []buffer.PlaneSpec
	scratch  []float32
	read     [][]float32
	written  [][]float32
	base     timing.OptionalPTS
	haveBase bool
	emitted  int64
	out      flow.Item[mediaaudio.Frame[float32]]
}

func newResampleOperator(plan resamplePlan, buffers *buffer.Allocator) *resampleOperator {
	return &resampleOperator{
		shape:    plan.shape.Clone(),
		buffers:  buffers,
		kernel:   newResampler(plan.inputRate, plan.targetRate, plan.channels),
		channels: plan.channels,
		maximum:  plan.samples,
		rates:    [2]int{plan.inputRate, plan.outputRate},
		planes:   make([]buffer.PlaneSpec, plan.channels),
		scratch:  make([]float32, plan.channels*plan.samples),
		read:     make([][]float32, plan.channels),
		written:  make([][]float32, plan.channels),
	}
}

func (o *resampleOperator) Ports() flow.Shape { return o.shape.Clone() }
func (o *resampleOperator) Close() error      { return nil }

func (o *resampleOperator) Process(ctx context.Context, input *flow.Item[mediaaudio.Frame[float32]], output flow.Emitter[mediaaudio.Frame[float32]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("resampler received an unowned frame")
	}
	frame := input.Value()
	if len(frame.Planes().Layout().Planes) != o.channels {
		return errFilterPlanes
	}
	samples := frame.Samples()
	if samples > o.maximum {
		return fmt.Errorf("%w: a frame of %d samples is past the %d this filter reserved", ErrUnsupported, samples, o.maximum)
	}
	if !o.haveBase {
		// The first frame fixes where the output starts, counted in the rate
		// the output is labelled with rather than the one it was read in.
		if value, ok := frame.PTS().Get(); ok {
			o.base = timing.SomePTS(timing.NewPTS(rescale(value.Int64(), o.rates[0], o.rates[1])))
		}
		o.haveBase = true
	}
	for channel := range o.channels {
		values, err := frame.PlaneSamples(channel)
		if err != nil {
			return err
		}
		window := o.scratch[channel*o.maximum : channel*o.maximum+samples]
		if values.CopyTo(window) != samples {
			return errFilterPlanes
		}
		o.read[channel] = window
	}
	return o.emit(ctx, output, o.kernel.capacity(samples), frame.SideData(), func(out [][]float32) int {
		return o.kernel.produce(out, o.read)
	})
}

func (o *resampleOperator) Flush(ctx context.Context, output flow.Emitter[mediaaudio.Frame[float32]]) error {
	pending := o.kernel.pending()
	if pending == 0 {
		return nil
	}
	return o.emit(ctx, output, pending, side.Data{}, o.kernel.drain)
}

func (o *resampleOperator) emit(ctx context.Context, output flow.Emitter[mediaaudio.Frame[float32]], capacity int, sideData side.Data, fill func([][]float32) int) error {
	if capacity == 0 {
		return nil
	}
	for index := range o.planes {
		o.planes[index].Size = capacity * 4
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 16, Planes: o.planes})
	if err != nil {
		return err
	}
	defer lease.Discard()
	written := 0
	if err := lease.Fill(func(storage buffer.Mutable) error {
		for channel := range o.channels {
			plane, planeErr := storage.Plane(channel)
			if planeErr != nil {
				return planeErr
			}
			values, castErr := mediaaudio.Plane[float32](plane, capacity)
			if castErr != nil {
				return castErr
			}
			o.written[channel] = values
		}
		written = fill(o.written)
		if written > capacity {
			return fmt.Errorf("%w: interpolation produced %d samples past its own bound of %d", ErrUnsupported, written, capacity)
		}
		return nil
	}); err != nil {
		return err
	}
	if written == 0 {
		return nil
	}
	storage, err := lease.Commit()
	if err != nil {
		return err
	}
	frame, err := mediaaudio.NewFrame[float32](o.timestamp(), written, storage)
	if err != nil {
		storage.Release()
		return err
	}
	o.emitted += int64(written)
	output.Own(&o.out, frame.WithSideData(sideData))
	defer o.out.Drop()
	return output.Emit(ctx, &o.out)
}

func (o *resampleOperator) timestamp() timing.OptionalPTS {
	value, ok := o.base.Get()
	if !ok {
		return timing.UnknownPTS()
	}
	return timing.SomePTS(timing.NewPTS(value.Int64() + o.emitted))
}
