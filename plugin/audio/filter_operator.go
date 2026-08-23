package audio

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
)

type filterOperator struct {
	shape    flow.Shape
	buffers  *buffer.Allocator
	kernel   filter
	channels int
	planes   [][]float32
	out      flow.Item[mediaaudio.Frame[float32]]
}

func newFilterOperator[C any](plan filterPlan[C], kernel filter, buffers *buffer.Allocator) *filterOperator {
	return &filterOperator{
		shape:    plan.shape.Clone(),
		buffers:  buffers,
		kernel:   kernel,
		channels: plan.channels,
		planes:   make([][]float32, 0, plan.channels),
	}
}

func (o *filterOperator) Ports() flow.Shape { return o.shape.Clone() }
func (o *filterOperator) Close() error      { return nil }

func (o *filterOperator) Process(ctx context.Context, input *flow.Item[mediaaudio.Frame[float32]], output flow.Emitter[mediaaudio.Frame[float32]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("filter received an unowned frame")
	}
	if err := flow.Transfer(input, &o.out, output, o.apply); err != nil {
		return err
	}
	defer o.out.Drop()
	return output.Emit(ctx, &o.out)
}

// apply changes the frame the filter was handed. One this branch owns alone is
// changed where it lies; one a fan-out left shared is copied first, so a
// sibling branch reading the same samples never sees them move under it. That
// copy is the only allocation a filter makes, and the grant it was compiled
// with is what bounds it.
func (o *filterOperator) apply(frame mediaaudio.Frame[float32]) (mediaaudio.Frame[float32], error) {
	var zero mediaaudio.Frame[float32]
	if len(frame.Planes().Layout().Planes) != o.channels {
		return zero, errFilterPlanes
	}
	editor, err := frame.Edit(o.buffers)
	if err != nil {
		if errors.Is(err, buffer.ErrLimit) {
			return zero, fmt.Errorf("%w: copying a shared %d-sample frame needs a larger maxSamples", err, frame.Samples())
		}
		return zero, err
	}
	o.planes = o.planes[:0]
	for channel := range o.channels {
		samples, err := editor.PlaneSamples(channel)
		if err != nil {
			editor.Discard()
			return zero, err
		}
		o.planes = append(o.planes, samples)
	}
	o.kernel.Apply(o.planes)
	candidate := editor.Frame()
	if err := editor.Commit(); err != nil {
		editor.Discard()
		return zero, err
	}
	return candidate, nil
}

func (*filterOperator) Flush(context.Context, flow.Emitter[mediaaudio.Frame[float32]]) error {
	return nil
}
