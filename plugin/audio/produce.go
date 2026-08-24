package audio

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/side"
	"github.com/godexture/godec/media/timing"
)

// A producer is a processor whose output is not the frame it was handed: it
// writes across a different set of channels, so there is nothing to edit in
// place and it fills planes of its own instead.
type producer interface {
	Produce(out, in [][]float32)
}

// producerOperator leases one output frame per input frame. The sample count
// and the timestamp stay the input's, because a processor that changed either
// would no longer be describing the same instants.
//
// The input is copied into scratch first. A borrowed frame exposes its samples
// as an immutable view rather than a slice, which is what stops a branch from
// writing through one, so a reader that wants a slice takes its own.
type producerOperator struct {
	shape   flow.Shape
	buffers *buffer.Allocator
	kernel  producer
	inputs  int
	outputs int
	maximum int
	planes  []buffer.PlaneSpec
	scratch []float32
	read    [][]float32
	written [][]float32
	out     flow.Item[mediaaudio.Frame[float32]]
}

func newProducerOperator(shape flow.Shape, kernel producer, inputs, outputs, maximum int, buffers *buffer.Allocator) *producerOperator {
	return &producerOperator{
		shape:   shape.Clone(),
		buffers: buffers,
		kernel:  kernel,
		inputs:  inputs,
		outputs: outputs,
		maximum: maximum,
		planes:  make([]buffer.PlaneSpec, outputs),
		scratch: make([]float32, inputs*maximum),
		read:    make([][]float32, inputs),
		written: make([][]float32, outputs),
	}
}

func (o *producerOperator) Ports() flow.Shape { return o.shape.Clone() }
func (o *producerOperator) Close() error      { return nil }

func (o *producerOperator) Process(ctx context.Context, input *flow.Item[mediaaudio.Frame[float32]], output flow.Emitter[mediaaudio.Frame[float32]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("filter received an unowned frame")
	}
	frame := input.Value()
	if frame.PlaneCount() != o.inputs {
		return errFilterPlanes
	}
	samples := frame.Samples()
	if samples > o.maximum {
		return fmt.Errorf("%w: a frame of %d samples is past the %d this filter reserved", ErrUnsupported, samples, o.maximum)
	}
	if err := o.take(frame, samples); err != nil {
		return err
	}
	produced, err := o.lease(samples, frame.PTS(), frame.SideData())
	if err != nil {
		return err
	}
	output.Own(&o.out, produced)
	defer o.out.Drop()
	return output.Emit(ctx, &o.out)
}

func (o *producerOperator) take(frame mediaaudio.Frame[float32], samples int) error {
	for channel := range o.inputs {
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
	return nil
}

func (o *producerOperator) lease(samples int, pts timing.OptionalPTS, sideData side.Data) (mediaaudio.Frame[float32], error) {
	for index := range o.planes {
		o.planes[index].Size = samples * 4
	}
	lease, err := o.buffers.Overwrite(buffer.Spec{Alignment: 16, Planes: o.planes})
	if err != nil {
		return mediaaudio.Frame[float32]{}, err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error {
		for channel := range o.outputs {
			plane, planeErr := storage.Plane(channel)
			if planeErr != nil {
				return planeErr
			}
			values, castErr := mediaaudio.Plane[float32](plane, samples)
			if castErr != nil {
				return castErr
			}
			o.written[channel] = values
		}
		o.kernel.Produce(o.written, o.read)
		return nil
	}); err != nil {
		return mediaaudio.Frame[float32]{}, err
	}
	storage, err := lease.Commit()
	if err != nil {
		return mediaaudio.Frame[float32]{}, err
	}
	produced, err := mediaaudio.NewFrame[float32](pts, samples, storage)
	if err != nil {
		storage.Release()
		return mediaaudio.Frame[float32]{}, err
	}
	return produced.WithSideData(sideData), nil
}

func (*producerOperator) Flush(context.Context, flow.Emitter[mediaaudio.Frame[float32]]) error {
	return nil
}

// reshaped is the description a producer's output carries: the same stream,
// stated across a different set of channels.
func reshaped(signal sample.Signal, layout sample.Layout) sample.Description {
	signal.Layout = layout
	return processed(signal)
}

func errFrameTooLarge(samples, maximum int) error {
	return fmt.Errorf("%w: a frame of %d samples is past the %d this filter reserved", ErrUnsupported, samples, maximum)
}

// frameLease builds one output frame and lets fill write its planes. Every
// stage that produces a frame rather than editing the one it read goes through
// here, so the ownership steps between leasing and emitting are written once.
type frameLease struct {
	buffers  *buffer.Allocator
	channels int
	planes   []buffer.PlaneSpec
	written  [][]float32
}

func newFrameLease(buffers *buffer.Allocator, channels int) *frameLease {
	return &frameLease{
		buffers:  buffers,
		channels: channels,
		planes:   make([]buffer.PlaneSpec, channels),
		written:  make([][]float32, channels),
	}
}

func (l *frameLease) build(samples int, pts timing.OptionalPTS, fill func([][]float32) error) (mediaaudio.Frame[float32], error) {
	var zero mediaaudio.Frame[float32]
	for index := range l.planes {
		l.planes[index].Size = samples * 4
	}
	lease, err := l.buffers.Overwrite(buffer.Spec{Alignment: 16, Planes: l.planes})
	if err != nil {
		return zero, err
	}
	defer lease.Discard()
	if err := lease.Fill(func(storage buffer.Mutable) error {
		for channel := range l.channels {
			plane, planeErr := storage.Plane(channel)
			if planeErr != nil {
				return planeErr
			}
			values, castErr := mediaaudio.Plane[float32](plane, samples)
			if castErr != nil {
				return castErr
			}
			l.written[channel] = values
		}
		return fill(l.written)
	}); err != nil {
		return zero, err
	}
	storage, err := lease.Commit()
	if err != nil {
		return zero, err
	}
	frame, err := mediaaudio.NewFrame[float32](pts, samples, storage)
	if err != nil {
		storage.Release()
		return zero, err
	}
	return frame, nil
}
