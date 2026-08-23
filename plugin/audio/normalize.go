package audio

import (
	"context"
	"errors"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type normalizeConfig struct {
	Target     config.DecibelValue
	Amplify    bool
	MaxSamples int
	MaxBytes   config.ByteValue
}

func normalizeSchema() config.Schema[normalizeConfig] {
	return config.Struct[normalizeConfigID](func() normalizeConfig {
		return normalizeConfig{Target: -1, Amplify: true, MaxSamples: defaultFilterSamples, MaxBytes: 1 << 30}
	}).
		Version("1").
		AddField(config.Field("target", func(value *normalizeConfig) *config.DecibelValue { return &value.Target },
			config.Decibel().Range(silenceFloorDB, 0).Help("level the loudest sample is brought to"))).
		AddField(config.Field("amplify", func(value *normalizeConfig) *bool { return &value.Amplify },
			config.Bool().Help("raise a stream already quieter than the target, rather than only lowering a loud one"))).
		AddField(budget(func(value *normalizeConfig) *int { return &value.MaxSamples })).
		AddField(holdingBytes(func(value *normalizeConfig) *config.ByteValue { return &value.MaxBytes },
			"most this filter holds while it looks for the loudest sample; the whole stream passes through it")).
		Build()
}

type normalizePlan struct {
	holdingPlan
	target  float32
	amplify bool
}

func newNormalize() plugin.Component {
	shape := filterShape()
	spec := plugin.Spec[normalizeConfig, normalizePlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration normalizeConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[normalizePlan, stream.Descriptor], error) {
			return compileHolding(shape, "audio.normalize", configuration.MaxSamples, configuration.MaxBytes, inputs,
				func(base holdingPlan) (normalizePlan, error) {
					return normalizePlan{
						holdingPlan: base,
						target:      amplitude(float64(configuration.Target)),
						amplify:     configuration.Amplify,
					}, nil
				})
		},
		Open: func(ctx plugin.OpenContext, plan normalizePlan) (flow.Operator, error) {
			held, err := openHolding(ctx, plan.holdingPlan)
			if err != nil {
				return nil, err
			}
			return &normalizeOperator{holding: held, target: plan.target, amplify: plan.amplify}, nil
		},
	}
	frames := sample.Frames[float32]()
	return plugin.NewComponent[normalizeID](plugin.Descriptor{DisplayName: "Normalize"}, normalizeSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("frames", frames, "filtered", frames),
	)
}

// normalizeOperator holds the stream because the level it applies depends on
// the loudest sample in it, and nothing knows that until the last one has
// passed. It emits nothing until then: there is no partial answer to give.
type normalizeOperator struct {
	*holding
	target  float32
	amplify bool
	peak    float32
}

func (o *normalizeOperator) Process(ctx context.Context, input *flow.Item[mediaaudio.Frame[float32]], _ flow.Emitter[mediaaudio.Frame[float32]]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("normalize received an unowned frame")
	}
	frame := input.Value()
	planes, err := o.store.read(frame, func(_ int, plane []float32) {
		o.peak = max(o.peak, peakOf(plane))
	})
	if err != nil {
		return err
	}
	return o.store.keep(ctx, frame.PTS(), planes)
}

func (o *normalizeOperator) Flush(ctx context.Context, output flow.Emitter[mediaaudio.Frame[float32]]) error {
	factor := o.factor()
	for index := range o.store.len() {
		record := o.store.at(index)
		frame, err := o.lease.build(record.samples, record.pts, func(planes [][]float32) error {
			if _, err := o.store.recall(ctx, index, planes); err != nil {
				return err
			}
			scale(planes, record.samples, factor)
			return nil
		})
		if err != nil {
			return err
		}
		output.Own(&o.out, frame)
		if err := output.Emit(ctx, &o.out); err != nil {
			o.out.Drop()
			return err
		}
		o.out.Drop()
	}
	return nil
}

// factor is what the loudest sample has to be multiplied by to reach the
// target. A silent stream has nothing to bring anywhere, and a stream already
// quieter than the target is only raised when raising it was asked for.
func (o *normalizeOperator) factor() float32 {
	if o.peak <= 0 {
		return 1
	}
	result := o.target / o.peak
	if !o.amplify && result > 1 {
		return 1
	}
	return result
}

func scale(planes [][]float32, samples int, factor float32) {
	if factor == 1 {
		return
	}
	for _, values := range planes {
		for position := range values[:samples] {
			values[position] *= factor
		}
	}
}
