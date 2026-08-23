package audio

import (
	"context"
	"errors"

	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/timing"
)

// relabelOperator moves frames through untouched and only recounts when they
// are presented. It is what a retime does when it is asked to play a stream at
// another rate rather than to resample it, and it is also what a resample does
// when the rate it was asked for is the one it already has -- both are the
// same statement, that these samples are the output samples.
type relabelOperator struct {
	shape flow.Shape
	from  int
	to    int
	out   flow.Item[mediaaudio.Frame[float32]]
}

func newRelabelOperator(shape flow.Shape, from, to int) *relabelOperator {
	return &relabelOperator{shape: shape.Clone(), from: from, to: to}
}

func (o *relabelOperator) Ports() flow.Shape { return o.shape.Clone() }
func (o *relabelOperator) Close() error      { return nil }

func (o *relabelOperator) Process(ctx context.Context, input *flow.Item[mediaaudio.Frame[float32]], output flow.Emitter[mediaaudio.Frame[float32]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("retime received an unowned frame")
	}
	if err := flow.Transfer(input, &o.out, output, o.relabel); err != nil {
		return err
	}
	defer o.out.Drop()
	return output.Emit(ctx, &o.out)
}

func (o *relabelOperator) relabel(frame mediaaudio.Frame[float32]) (mediaaudio.Frame[float32], error) {
	value, ok := frame.PTS().Get()
	if !ok || o.from == o.to {
		return frame, nil
	}
	return frame.WithPTS(timing.SomePTS(timing.NewPTS(rescale(value.Int64(), o.from, o.to)))), nil
}

func (*relabelOperator) Flush(context.Context, flow.Emitter[mediaaudio.Frame[float32]]) error {
	return nil
}
