package audio

import (
	"context"

	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/timing"
)

// mixerOperator adds its inputs together sample for sample. One round brings
// one frame from every input that has not ended, and those frames need not be
// the same length, so it keeps what it cannot yet pair and emits only as far
// as every input it is still waiting for has reached.
//
// An input that has ended contributes silence rather than stopping the stream,
// which is what makes the result as long as the longest of them.
type mixerOperator struct {
	shape    flow.Shape
	lease    *frameLease
	weights  []float32
	channels int
	maximum  int
	// pending is what each input has delivered and this mixer has not paired
	// yet, one slice per channel.
	pending  [][][]float32
	ended    []bool
	base     timing.OptionalPTS
	haveBase bool
	emitted  int64
	out      flow.Item[mediaaudio.Frame[float32]]
}

func newMixerOperator(plan mixerPlan, buffers *buffer.Allocator) *mixerOperator {
	pending := make([][][]float32, len(plan.weights))
	for index := range pending {
		pending[index] = make([][]float32, plan.channels)
	}
	return &mixerOperator{
		shape:    plan.shape.Clone(),
		lease:    newFrameLease(buffers, plan.channels),
		weights:  plan.weights,
		channels: plan.channels,
		maximum:  plan.samples,
		pending:  pending,
		ended:    make([]bool, len(plan.weights)),
	}
}

func (o *mixerOperator) Ports() flow.Shape { return o.shape.Clone() }
func (o *mixerOperator) Close() error      { return nil }

func (o *mixerOperator) Process(ctx context.Context, batch flow.Batch[mediaaudio.Frame[float32]], output flow.Emitter[mediaaudio.Frame[float32]]) error {
	for index := range min(batch.Len(), len(o.pending)) {
		item := batch.At(index)
		if !item.Valid() {
			// This input has ended. It goes on contributing silence, so the
			// others are not cut short by it.
			o.ended[index] = true
			continue
		}
		err := o.take(index, item.Value())
		item.Drop()
		if err != nil {
			return err
		}
	}
	return o.mix(ctx, output)
}

// Flush marks every input ended and drains what is left. By now nothing more
// can arrive, so what remains of the longest input is mixed against the
// silence the others became.
func (o *mixerOperator) Flush(ctx context.Context, output flow.Emitter[mediaaudio.Frame[float32]]) error {
	for index := range o.ended {
		o.ended[index] = true
	}
	return o.mix(ctx, output)
}

func (o *mixerOperator) take(index int, frame mediaaudio.Frame[float32]) error {
	if len(frame.Planes().Layout().Planes) != o.channels {
		return errFilterPlanes
	}
	if !o.haveBase {
		o.base, o.haveBase = frame.PTS(), true
	}
	for channel := range o.channels {
		values, err := frame.PlaneSamples(channel)
		if err != nil {
			return err
		}
		o.pending[index][channel] = values.AppendTo(o.pending[index][channel])
	}
	return nil
}

// mix emits as much as it can and no more. What it can is bounded by the input
// that has delivered the least of the ones it is still waiting for; an input
// that has ended and been drained is silence from here on and never holds the
// others up.
func (o *mixerOperator) mix(ctx context.Context, output flow.Emitter[mediaaudio.Frame[float32]]) error {
	for {
		length := -1
		for index := range o.pending {
			available := len(o.pending[index][0])
			if o.ended[index] && available == 0 {
				continue
			}
			if length == -1 || available < length {
				length = available
			}
		}
		if length <= 0 {
			return nil
		}
		length = min(length, o.maximum)
		frame, err := o.lease.build(length, o.timestamp(), func(planes [][]float32) error {
			o.sum(planes, length)
			return nil
		})
		if err != nil {
			return err
		}
		o.consume(length)
		o.emitted += int64(length)
		output.Own(&o.out, frame)
		if err := output.Emit(ctx, &o.out); err != nil {
			o.out.Drop()
			return err
		}
		o.out.Drop()
	}
}

func (o *mixerOperator) sum(planes [][]float32, length int) {
	for _, target := range planes {
		clear(target[:length])
	}
	for index, weight := range o.weights {
		if weight == 0 {
			continue
		}
		for channel, source := range o.pending[index] {
			if len(source) == 0 {
				continue
			}
			target := planes[channel]
			for position := range min(length, len(source)) {
				target[position] += source[position] * weight
			}
		}
	}
}

func (o *mixerOperator) consume(length int) {
	for index := range o.pending {
		for channel, source := range o.pending[index] {
			if len(source) <= length {
				o.pending[index][channel] = source[:0]
				continue
			}
			o.pending[index][channel] = append(source[:0], source[length:]...)
		}
	}
}

func (o *mixerOperator) timestamp() timing.OptionalPTS {
	value, ok := o.base.Get()
	if !ok {
		return timing.UnknownPTS()
	}
	return timing.SomePTS(timing.NewPTS(value.Int64() + o.emitted))
}
