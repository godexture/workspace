package audio

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/timing"
)

// convolverOperator reads its two inputs in the order the ports declared: the
// impulse response entire, then the signal. Which cell a round carries says
// which it is, because the response's port has ended by the time the signal's
// starts.
type convolverOperator struct {
	shape    flow.Shape
	lease    *frameLease
	plan     convolverPlan
	kernel   *convolver
	impulse  [][]float32
	pending  [][]float32
	built    bool
	base     timing.OptionalPTS
	haveBase bool
	emitted  int64
	out      flow.Item[mediaaudio.Frame[float32]]
}

func newConvolverOperator(plan convolverPlan, buffers *buffer.Allocator) *convolverOperator {
	return &convolverOperator{
		shape:   plan.shape.Clone(),
		lease:   newFrameLease(buffers, plan.channels),
		plan:    plan,
		pending: make([][]float32, plan.channels),
	}
}

func (o *convolverOperator) Ports() flow.Shape { return o.shape.Clone() }
func (o *convolverOperator) Close() error      { return nil }

func (o *convolverOperator) Process(ctx context.Context, batch flow.Batch[mediaaudio.Frame[float32]], output flow.Emitter[mediaaudio.Frame[float32]]) error {
	if item := batch.At(0); item.Valid() {
		err := o.collect(item.Value())
		item.Drop()
		return err
	}
	item := batch.At(1)
	if !item.Valid() {
		return nil
	}
	// The response's port has ended, so this is the first signal frame and the
	// response is whatever was collected.
	if err := o.build(); err != nil {
		item.Drop()
		return err
	}
	err := o.accept(item.Value())
	item.Drop()
	if err != nil {
		return err
	}
	return o.drain(ctx, output, o.plan.hop)
}

// Flush pads the signal out to a whole hop and then by the length of the
// response, so the part of the result that outlives its input is emitted
// rather than cut off.
func (o *convolverOperator) Flush(ctx context.Context, output flow.Emitter[mediaaudio.Frame[float32]]) error {
	if !o.built {
		return nil
	}
	padding := o.kernel.tail * o.plan.hop
	if remainder := len(o.pending[0]) % o.plan.hop; remainder != 0 {
		padding += o.plan.hop - remainder
	}
	for channel := range o.pending {
		o.pending[channel] = append(o.pending[channel], make([]float32, padding)...)
	}
	return o.drain(ctx, output, o.plan.hop)
}

func (o *convolverOperator) collect(frame mediaaudio.Frame[float32]) error {
	planes := frame.PlaneCount()
	if o.impulse == nil {
		o.impulse = make([][]float32, planes)
	}
	if len(o.impulse) != planes {
		return errors.New("the impulse response changed its channel count within the stream")
	}
	for channel := range planes {
		values, err := frame.PlaneSamples(channel)
		if err != nil {
			return err
		}
		o.impulse[channel] = values.AppendTo(o.impulse[channel])
	}
	return nil
}

func (o *convolverOperator) build() error {
	if o.built {
		return nil
	}
	if len(o.impulse) == 0 || len(o.impulse[0]) == 0 {
		return fmt.Errorf("%w: the impulse response input carried no samples", ErrUnsupported)
	}
	kernel, err := newConvolver(o.impulse, o.plan.hop, o.plan.mix, o.plan.normalize)
	if err != nil {
		return err
	}
	if count := kernel.channelCount(); count != 1 && count != o.plan.channels {
		return fmt.Errorf("%w: a %d-channel response cannot filter %d channels", ErrUnsupported, count, o.plan.channels)
	}
	kernel.prepare(o.plan.channels)
	o.kernel, o.built, o.impulse = kernel, true, nil
	return nil
}

func (o *convolverOperator) accept(frame mediaaudio.Frame[float32]) error {
	if frame.PlaneCount() != o.plan.channels {
		return errFilterPlanes
	}
	if !o.haveBase {
		o.base, o.haveBase = frame.PTS(), true
	}
	for channel := range o.plan.channels {
		values, err := frame.PlaneSamples(channel)
		if err != nil {
			return err
		}
		o.pending[channel] = values.AppendTo(o.pending[channel])
	}
	return nil
}

// drain convolves every whole hop that has arrived and emits it. A hop is the
// unit the transform works in, so what is left over waits for the samples that
// complete it.
func (o *convolverOperator) drain(ctx context.Context, output flow.Emitter[mediaaudio.Frame[float32]], hop int) error {
	for len(o.pending[0]) >= hop {
		frame, err := o.lease.build(hop, o.timestamp(), func(planes [][]float32) error {
			for channel := range o.plan.channels {
				copy(planes[channel][:hop], o.pending[channel][:hop])
				if err := o.kernel.hopOf(channel, planes[channel][:hop]); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		for channel := range o.pending {
			o.pending[channel] = append(o.pending[channel][:0], o.pending[channel][hop:]...)
		}
		o.emitted += int64(hop)
		output.Own(&o.out, frame)
		if err := output.Emit(ctx, &o.out); err != nil {
			o.out.Drop()
			return err
		}
		o.out.Drop()
	}
	return nil
}

func (o *convolverOperator) timestamp() timing.OptionalPTS {
	value, ok := o.base.Get()
	if !ok {
		return timing.UnknownPTS()
	}
	return timing.SomePTS(timing.NewPTS(value.Int64() + o.emitted))
}
