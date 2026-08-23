package audio

import (
	"math"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

// retimeMode picks which of the two things "play this faster" can mean.
type retimeMode string

const (
	// interpolateRetime resamples, so the stream keeps the rate it is counted
	// in and has fewer or more samples in it than it started with.
	interpolateRetime retimeMode = "interpolate"
	// relabelRetime moves no samples at all and states them at another rate,
	// which loses nothing and is the only exact way to do this.
	relabelRetime retimeMode = "relabel"
)

type retimeConfig struct {
	Factor     config.RatioValue
	Mode       retimeMode
	MaxSamples int
}

func retimeSchema() config.Schema[retimeConfig] {
	return config.Struct[retimeConfigID](func() retimeConfig {
		return retimeConfig{Factor: 1, Mode: interpolateRetime, MaxSamples: defaultFilterSamples}
	}).
		Version("1").
		AddField(config.Field("factor", func(value *retimeConfig) *config.RatioValue { return &value.Factor },
			config.Ratio().Range(0.01, 100).
				Help("how much faster the stream plays; pitch moves with it, because nothing here stretches time alone"))).
		AddField(config.Field("mode", func(value *retimeConfig) *retimeMode { return &value.Mode },
			config.Enum(
				config.Choice[retimeMode]{ID: string(interpolateRetime), Label: "Interpolate", Value: interpolateRetime},
				config.Choice[retimeMode]{ID: string(relabelRetime), Label: "Relabel", Value: relabelRetime},
			).Help("whether to resample at the rate it is counted in, or to keep the samples and count them at another rate"))).
		AddField(budget(func(value *retimeConfig) *int { return &value.MaxSamples })).
		Build()
}

func newRetime() plugin.Component {
	shape := filterShape()
	spec := plugin.Spec[retimeConfig, resamplePlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration retimeConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[resamplePlan, stream.Descriptor], error) {
			return compileRetime(shape, configuration, inputs)
		},
		Open: openResample,
	}
	frames := sample.Frames[float32]()
	return plugin.NewComponent[retimeID](plugin.Descriptor{DisplayName: "Retime"}, retimeSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("frames", frames, "filtered", frames),
	)
}

func compileRetime(shape flow.Shape, configuration retimeConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[resamplePlan, stream.Descriptor], error) {
	input, signal, incomplete, ready, err := processedInput[resamplePlan](shape, inputs)
	if !ready || err != nil {
		return incomplete, err
	}
	plan := resamplePlan{
		shape:      shape.Clone(),
		inputRate:  signal.Rate,
		targetRate: signal.Rate,
		outputRate: signal.Rate,
		channels:   signal.Layout.Count(),
		samples:    configuration.MaxSamples,
		detail:     "audio.retime",
	}
	counted := signal.Rate
	switch {
	case configuration.Factor == 1:
	case configuration.Mode == relabelRetime:
		// Counting the same samples at a higher rate is what makes them pass
		// sooner. Nothing is interpolated, so the operator only recounts.
		counted = scaledRate(signal.Rate, float64(configuration.Factor))
		plan.outputRate = counted
	default:
		// Interpolating toward a lower rate and then counting the result at
		// the original one leaves fewer samples covering the same instants,
		// which is the same thing heard the other way round.
		plan.targetRate = scaledRate(signal.Rate, 1/float64(configuration.Factor))
	}
	return retimed(shape, input, signal, plan, counted)
}

func scaledRate(rate int, factor float64) int {
	return max(int(math.Round(float64(rate)*factor)), 1)
}
