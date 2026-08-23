package audio

import (
	"context"
	"errors"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

// trimMode says which ends of the stream lose their silence.
type trimMode string

const (
	trimBoth  trimMode = "both"
	trimStart trimMode = "start"
	trimEnd   trimMode = "end"
)

type trimConfig struct {
	Threshold  config.DecibelValue
	Mode       trimMode
	MaxSamples int
	MaxBytes   config.ByteValue
}

func trimSchema() config.Schema[trimConfig] {
	return config.Struct[trimConfigID](func() trimConfig {
		return trimConfig{Threshold: -60, Mode: trimBoth, MaxSamples: defaultFilterSamples, MaxBytes: 64 << 20}
	}).
		Version("1").
		AddField(config.Field("threshold", func(value *trimConfig) *config.DecibelValue { return &value.Threshold },
			config.Decibel().Range(silenceFloorDB, 0).Help("level below which a sample counts as silence"))).
		AddField(config.Field("mode", func(value *trimConfig) *trimMode { return &value.Mode },
			config.Enum(
				config.Choice[trimMode]{ID: string(trimBoth), Label: "Both ends", Value: trimBoth},
				config.Choice[trimMode]{ID: string(trimStart), Label: "Start", Value: trimStart},
				config.Choice[trimMode]{ID: string(trimEnd), Label: "End", Value: trimEnd},
			).Help("which ends to trim"))).
		AddField(budget(func(value *trimConfig) *int { return &value.MaxSamples })).
		AddField(holdingBytes(func(value *trimConfig) *config.ByteValue { return &value.MaxBytes },
			"most this filter holds while it waits to learn whether a quiet run was the end of the stream")).
		Build()
}

type trimPlan struct {
	holdingPlan
	threshold float32
	leading   bool
	trailing  bool
}

func newTrim() plugin.Component {
	shape := filterShape()
	spec := plugin.Spec[trimConfig, trimPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration trimConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[trimPlan, stream.Descriptor], error) {
			return compileHolding(shape, "audio.trim", configuration.MaxSamples, configuration.MaxBytes, inputs,
				func(base holdingPlan) (trimPlan, error) {
					return trimPlan{
						holdingPlan: base,
						threshold:   amplitude(float64(configuration.Threshold)),
						leading:     configuration.Mode != trimEnd,
						trailing:    configuration.Mode != trimStart,
					}, nil
				})
		},
		Open: func(ctx plugin.OpenContext, plan trimPlan) (flow.Operator, error) {
			held, err := openHolding(ctx, plan.holdingPlan)
			if err != nil {
				return nil, err
			}
			return &trimOperator{
				holding:   held,
				threshold: plan.threshold,
				leading:   plan.leading,
				trailing:  plan.trailing,
				started:   !plan.leading,
			}, nil
		},
	}
	frames := sample.Frames[float32]()
	return plugin.NewComponent[trimID](plugin.Descriptor{DisplayName: "Trim"}, trimSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("frames", frames, "filtered", frames),
	)
}

// trimOperator drops the silence at the ends of a stream. Its two halves are
// not symmetric: the start is decided as soon as something is heard, while the
// end is only ever a guess -- a quiet run is the end of the stream until
// something else arrives, so it is held rather than emitted, and let go of the
// moment it turns out to have been a gap.
type trimOperator struct {
	*holding
	threshold float32
	leading   bool
	trailing  bool
	started   bool
}

func (o *trimOperator) Process(ctx context.Context, input *flow.Item[mediaaudio.Frame[float32]], output flow.Emitter[mediaaudio.Frame[float32]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("trim received an unowned frame")
	}
	frame := input.Value()
	first, last := -1, -1
	planes, err := o.store.read(frame, func(_ int, plane []float32) {
		low, high := activity(plane, o.threshold)
		if low < 0 {
			return
		}
		if first < 0 || low < first {
			first = low
		}
		if high > last {
			last = high
		}
	})
	if err != nil {
		return err
	}
	start := 0
	if !o.started {
		if last < 0 {
			return nil
		}
		o.started, start = true, first
	}
	if !o.trailing {
		return o.send(ctx, output, frame.PTS(), planes, start, len(planes[0]))
	}
	if last < 0 {
		return o.store.keep(ctx, frame.PTS(), window(planes, start, len(planes[0])))
	}
	// Something was heard, so everything held was a gap rather than the end.
	if err := o.replay(ctx, output); err != nil {
		return err
	}
	if err := o.send(ctx, output, frame.PTS(), planes, start, last+1); err != nil {
		return err
	}
	if last+1 == len(planes[0]) {
		return nil
	}
	return o.store.keep(ctx, shifted(frame.PTS(), last+1), window(planes, last+1, len(planes[0])))
}

// Flush drops whatever is still held: nothing arrived after it, so the quiet
// run it was holding really was the end of the stream.
func (*trimOperator) Flush(context.Context, flow.Emitter[mediaaudio.Frame[float32]]) error {
	return nil
}

func (o *trimOperator) replay(ctx context.Context, output flow.Emitter[mediaaudio.Frame[float32]]) error {
	for index := range o.store.len() {
		record := o.store.at(index)
		frame, err := o.lease.build(record.samples, record.pts, func(planes [][]float32) error {
			_, err := o.store.recall(ctx, index, planes)
			return err
		})
		if err != nil {
			return err
		}
		if err := o.emit(ctx, output, frame); err != nil {
			return err
		}
	}
	o.store.rewind()
	return nil
}

func (o *trimOperator) send(ctx context.Context, output flow.Emitter[mediaaudio.Frame[float32]], pts timing.OptionalPTS, planes [][]float32, from, to int) error {
	if to <= from {
		return nil
	}
	frame, err := o.lease.build(to-from, shifted(pts, from), func(target [][]float32) error {
		for channel, values := range planes {
			copy(target[channel][:to-from], values[from:to])
		}
		return nil
	})
	if err != nil {
		return err
	}
	return o.emit(ctx, output, frame)
}

func (o *trimOperator) emit(ctx context.Context, output flow.Emitter[mediaaudio.Frame[float32]], frame mediaaudio.Frame[float32]) error {
	output.Own(&o.out, frame)
	defer o.out.Drop()
	return output.Emit(ctx, &o.out)
}

// activity reports the first and last position of a plane that is at or above
// the threshold, or -1 for a plane that never is.
func activity(plane []float32, threshold float32) (int, int) {
	first, last := -1, -1
	for index, value := range plane {
		if value < 0 {
			value = -value
		}
		if value < threshold {
			continue
		}
		if first < 0 {
			first = index
		}
		last = index
	}
	return first, last
}

func window(planes [][]float32, from, to int) [][]float32 {
	result := make([][]float32, len(planes))
	for channel, values := range planes {
		result[channel] = values[from:to]
	}
	return result
}

func shifted(pts timing.OptionalPTS, by int) timing.OptionalPTS {
	value, ok := pts.Get()
	if !ok || by == 0 {
		return pts
	}
	return timing.SomePTS(timing.NewPTS(value.Int64() + int64(by)))
}
