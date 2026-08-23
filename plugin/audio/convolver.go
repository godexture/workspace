package audio

import (
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

const defaultConvolverHop = 4096

type convolverConfig struct {
	Mix        config.RatioValue
	Normalize  bool
	BlockSize  int
	MaxSamples int
}

func convolverSchema() config.Schema[convolverConfig] {
	return config.Struct[convolverConfigID](func() convolverConfig {
		return convolverConfig{Mix: 1, Normalize: true, BlockSize: defaultConvolverHop, MaxSamples: defaultFilterSamples}
	}).
		Version("1").
		AddField(config.Field("mix", func(value *convolverConfig) *config.RatioValue { return &value.Mix },
			config.Ratio().Range(0, 1).Help("balance between the signal as it arrived and the convolved one"))).
		AddField(config.Field("normalize", func(value *convolverConfig) *bool { return &value.Normalize },
			config.Bool().Help("scale the response down where its gain could push the result past full scale"))).
		AddField(config.Field("blockSize", func(value *convolverConfig) *int { return &value.BlockSize },
			config.Int().Range(64, 1<<16).
				Help("samples per transform, rounded up to a power of two; it is also the latency the filter adds"))).
		AddField(budget(func(value *convolverConfig) *int { return &value.MaxSamples })).
		Build()
}

type convolverPlan struct {
	shape     flow.Shape
	hop       int
	mix       float32
	normalize bool
	channels  int
	samples   int
}

// convolverShape gives the impulse response an input port of its own rather
// than another edge of the signal's. Two reasons, and both need the port: the
// planner bridges an edge only where a port has exactly one, so a response
// recorded at another rate is resampled on the way in; and a port can declare
// that it comes first, which is what lets the whole response be read before a
// sample of signal is, without holding the signal in memory to do it.
func convolverShape() flow.Shape {
	frames := sample.Frames[float32]()
	return flow.NewShape(
		[]flow.Port{flow.In("ir", frames, flow.Prior()), flow.In("in", frames)},
		[]flow.Port{flow.Out("convolved", frames)},
	)
}

func newConvolverComponent() plugin.Component {
	shape := convolverShape()
	frames := sample.Frames[float32]()
	spec := plugin.Spec[convolverConfig, convolverPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration convolverConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[convolverPlan, stream.Descriptor], error) {
			return compileConvolver(shape, configuration, inputs)
		},
		Open: func(ctx plugin.OpenContext, plan convolverPlan) (flow.Operator, error) {
			if ctx.Buffers() == nil {
				return nil, fmt.Errorf("%w: a filter requires a payload buffer grant", ErrUnsupported)
			}
			return newConvolverOperator(plan, ctx.Buffers()), nil
		},
	}
	return plugin.NewComponent[convolverID](plugin.Descriptor{DisplayName: "Convolver"}, convolverSchema(),
		plugin.WithSpec(spec),
		plugin.WithFanIn(shape.Inputs, frames, flow.ZipFanIn, "convolved", frames),
	)
}

func compileConvolver(shape flow.Shape, configuration convolverConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[convolverPlan, stream.Descriptor], error) {
	signalPort, impulsePort := shape.Inputs[1], shape.Inputs[0]
	input, signal, incomplete, ready, err := processedPort[convolverPlan](shape, signalPort, inputs)
	if !ready || err != nil {
		return incomplete, err
	}
	impulse, response, incomplete, ready, err := processedPort[convolverPlan](shape, impulsePort, inputs)
	if !ready || err != nil {
		return incomplete, err
	}
	// A response recorded at another rate is the same room heard at the wrong
	// speed, so the rate has to match and the planner is asked to make it so
	// rather than the convolver refusing what it was given.
	if response.Rate != signal.Rate {
		wanted := response
		wanted.Rate = signal.Rate
		desired, desiredErr := describeProcessed(impulse, impulsePort.Schema(), wanted)
		if desiredErr != nil {
			return plugin.Compiled[convolverPlan, stream.Descriptor]{}, desiredErr
		}
		return plugin.Compiled[convolverPlan, stream.Descriptor]{
			Requirements: []plugin.Requirement[stream.Descriptor]{
				plugin.Require(impulsePort.ID(), plugin.DescriptorNeed("audio.convolver-rate", desired)),
			},
		}, nil
	}
	channels := signal.Layout.Count()
	// One response channel applies to every input channel; one each convolves
	// them independently. Anything between leaves channels unaccounted for.
	if response.Layout.Count() != 1 && response.Layout.Count() != channels {
		return plugin.Compiled[convolverPlan, stream.Descriptor]{}, fmt.Errorf(
			"%w: a %d-channel response cannot filter %d channels", ErrUnsupported, response.Layout.Count(), channels)
	}
	// The result runs on past the signal by the length of the response, and
	// how long that is only the response knows, so the length the signal
	// stated stops being true here.
	output, err := stream.NewDescriptor(input.ID(), shape.Outputs[0].Schema(), input.TimeBase(),
		stream.WithoutDuration(input.Properties()))
	if err != nil {
		return plugin.Compiled[convolverPlan, stream.Descriptor]{}, err
	}
	hop := nextPowerOfTwo(configuration.BlockSize)
	return plugin.Compiled[convolverPlan, stream.Descriptor]{
		Plan: convolverPlan{
			shape:     shape.Clone(),
			hop:       hop,
			mix:       float32(configuration.Mix),
			normalize: configuration.Normalize,
			channels:  channels,
			samples:   max(configuration.MaxSamples, hop),
		},
		Outputs:   flow.NewDescriptors(flow.Describe("convolved", output.WithMetadata(input.Metadata()))),
		Effects:   []plugin.Effect{{Kind: plugin.ContentEffect, Loss: plugin.Lossy, Detail: "audio.convolve"}},
		Resources: resource.Request{Memory: resource.Bytes(planeBytes[float32](max(configuration.MaxSamples, hop), channels))},
	}, nil
}
