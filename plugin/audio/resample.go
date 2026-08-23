package audio

import (
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type resampleConfig struct {
	Rate       config.Rate
	MaxSamples int
}

func resampleSchema() config.Schema[resampleConfig] {
	return config.Struct[resampleConfigID](func() resampleConfig {
		return resampleConfig{Rate: config.AutoRate(), MaxSamples: defaultFilterSamples}
	}).
		Version("1").
		AddField(config.Field("rate", func(value *resampleConfig) *config.Rate { return &value.Rate },
			config.RateCodec().Help("rate the stream is counted in afterwards, or auto to keep the one it arrived with"))).
		AddField(budget(func(value *resampleConfig) *int { return &value.MaxSamples })).
		Build()
}

func newResample() plugin.Component {
	shape := filterShape()
	spec := plugin.Spec[resampleConfig, resamplePlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration resampleConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[resamplePlan, stream.Descriptor], error) {
			return compileResample(shape, configuration, inputs)
		},
		Open: openResample,
	}
	frames := sample.Frames[float32]()
	return plugin.NewComponent[resampleID](plugin.Descriptor{DisplayName: "Resample"}, resampleSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("frames", frames, "filtered", frames),
	)
}

func openResample(ctx plugin.OpenContext, plan resamplePlan) (flow.Operator, error) {
	if plan.inputRate == plan.targetRate {
		return newRelabelOperator(plan.shape, plan.inputRate, plan.outputRate), nil
	}
	if ctx.Buffers() == nil {
		return nil, fmt.Errorf("%w: a filter requires a payload buffer grant", ErrUnsupported)
	}
	return newResampleOperator(plan, ctx.Buffers()), nil
}

func compileResample(shape flow.Shape, configuration resampleConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[resamplePlan, stream.Descriptor], error) {
	input, signal, incomplete, ready, err := processedInput[resamplePlan](shape, inputs)
	if !ready || err != nil {
		return incomplete, err
	}
	// A rate left on auto is a request to keep the one the stream arrived
	// with, which makes this stage a no-op rather than a missing setting.
	target := signal.Rate
	if configuration.Rate.Mode == config.RateModeFixed {
		target = int(configuration.Rate.Hertz)
	}
	if target <= 0 {
		return plugin.Compiled[resamplePlan, stream.Descriptor]{}, fmt.Errorf("%w: a rate of %d is not one a stream can be counted in", ErrUnsupported, target)
	}
	return retimed(shape, input, signal, resamplePlan{
		shape:      shape.Clone(),
		inputRate:  signal.Rate,
		targetRate: target,
		outputRate: target,
		channels:   signal.Layout.Count(),
		samples:    configuration.MaxSamples,
		detail:     "audio.resample",
	}, target)
}

// retimed describes the stream on the far side of a stage that changes how
// many samples carry it. counted is the rate the result is stated in, which is
// the target for a resample and the original for a retime.
func retimed(shape flow.Shape, input stream.Descriptor, signal sample.Signal, plan resamplePlan, counted int) (plugin.Compiled[resamplePlan, stream.Descriptor], error) {
	signal.Rate = counted
	properties, err := processed(signal).Apply(input.Properties())
	if err != nil {
		return plugin.Compiled[resamplePlan, stream.Descriptor]{}, err
	}
	output, err := stream.NewDescriptor(input.ID(), shape.Outputs[0].Schema(), timing.MustBase(1, int64(counted)), properties)
	if err != nil {
		return plugin.Compiled[resamplePlan, stream.Descriptor]{}, err
	}
	// Interpolating between samples cannot restore what fell between them, so
	// only a stage that moves no samples at all is free of loss.
	loss := plugin.Lossy
	if plan.inputRate == plan.targetRate {
		loss = plugin.NoLoss
	}
	return plugin.Compiled[resamplePlan, stream.Descriptor]{
		Plan:      plan,
		Outputs:   flow.NewDescriptors(flow.Describe(shape.Outputs[0].ID(), output.WithMetadata(input.Metadata()))),
		Effects:   []plugin.Effect{{Kind: plugin.TimelineEffect, Loss: loss, Detail: plan.detail}},
		Resources: resource.Request{Memory: resource.Bytes(planeBytes[float32](resampleReserve(plan), plan.channels))},
	}, nil
}

// resampleReserve is the largest output frame the interpolation can lease:
// upsampling produces more samples than it read, and the lease has to hold
// them.
func resampleReserve(plan resamplePlan) int {
	if plan.inputRate == plan.targetRate {
		return plan.samples
	}
	return newResampler(plan.inputRate, plan.targetRate, plan.channels).capacity(plan.samples)
}
