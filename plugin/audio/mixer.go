package audio

import (
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type mixerConfig struct {
	Weights    []config.RatioValue
	MaxSamples int
}

func mixerSchema() config.Schema[mixerConfig] {
	return config.Struct[mixerConfigID](func() mixerConfig {
		return mixerConfig{MaxSamples: defaultFilterSamples}
	}).
		Version("1").
		AddField(config.Field("weights", func(value *mixerConfig) *[]config.RatioValue { return &value.Weights },
			config.Slice(config.Ratio().Range(-4, 4)).
				Help("level each input is mixed at, in the order they are connected, or empty to take them all whole"))).
		AddField(budget(func(value *mixerConfig) *int { return &value.MaxSamples })).
		Build()
}

type mixerPlan struct {
	shape    flow.Shape
	weights  []float32
	channels int
	samples  int
}

// newMixer takes any number of inputs and produces one stream. A mixer that
// also fanned its result out to several outputs used to be the same component;
// it is not one here, because duplicating an output is what the runtime does
// for every edge that has more than one consumer, and a matrix of weights is
// one of these per row.
func newMixer() plugin.Component {
	frames := sample.Frames[float32]()
	shape := flow.NewShape(
		[]flow.Port{flow.In("inputs", frames, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("mixed", frames)},
	)
	spec := plugin.Spec[mixerConfig, mixerPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration mixerConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[mixerPlan, stream.Descriptor], error) {
			return compileMixer(shape, configuration, inputs)
		},
		Open: func(ctx plugin.OpenContext, plan mixerPlan) (flow.Operator, error) {
			if ctx.Buffers() == nil {
				return nil, fmt.Errorf("%w: a filter requires a payload buffer grant", ErrUnsupported)
			}
			return newMixerOperator(plan, ctx.Buffers()), nil
		},
	}
	return plugin.NewComponent[mixerID](plugin.Descriptor{DisplayName: "Mixer"}, mixerSchema(),
		plugin.WithSpec(spec),
		plugin.WithJoiner("inputs", frames, flow.ZipFanIn, "mixed", frames),
	)
}

func compileMixer(shape flow.Shape, configuration mixerConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[mixerPlan, stream.Descriptor], error) {
	connected := inputs.At("inputs")
	if len(connected) == 0 {
		return plugin.Compiled[mixerPlan, stream.Descriptor]{
			Requirements: []plugin.Requirement[stream.Descriptor]{
				plugin.Require("inputs", plugin.ConditionNeed[stream.Descriptor]("audio.mixer-input")),
			},
		}, nil
	}
	// Every input is mixed sample for sample, so they have to be counting in
	// the same rate across the same channels. Reconciling either is what the
	// resampler and the remix are for, and asking for them here is what puts
	// them in the graph where they can be seen.
	first, signal, incomplete, ready, err := processedInput[mixerPlan](shape, flow.NewDescriptors(flow.Describe("inputs", connected[0])))
	if !ready || err != nil {
		return incomplete, err
	}
	for _, descriptor := range connected[1:] {
		other, otherErr := sample.SignalOf(descriptor.Properties())
		if otherErr != nil || other.Rate != signal.Rate || other.Layout != signal.Layout {
			desired, desiredErr := describeProcessed(descriptor, shape.Inputs[0].Schema(), signal)
			if desiredErr != nil {
				return plugin.Compiled[mixerPlan, stream.Descriptor]{}, desiredErr
			}
			return plugin.Compiled[mixerPlan, stream.Descriptor]{
				Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("inputs", plugin.DescriptorNeed("audio.mixer-agreement", desired)),
				},
			}, nil
		}
	}
	weights, err := mixerWeights(configuration.Weights, len(connected))
	if err != nil {
		return plugin.Compiled[mixerPlan, stream.Descriptor]{}, err
	}
	properties, err := mixedLength(first.Properties(), connected)
	if err != nil {
		return plugin.Compiled[mixerPlan, stream.Descriptor]{}, err
	}
	output, err := stream.NewDescriptor(first.ID(), shape.Outputs[0].Schema(), first.TimeBase(), properties)
	if err != nil {
		return plugin.Compiled[mixerPlan, stream.Descriptor]{}, err
	}
	channels := signal.Layout.Count()
	return plugin.Compiled[mixerPlan, stream.Descriptor]{
		Plan: mixerPlan{
			shape:    shape.Clone(),
			weights:  weights,
			channels: channels,
			samples:  configuration.MaxSamples,
		},
		Outputs:   flow.NewDescriptors(flow.Describe("mixed", output.WithMetadata(first.Metadata()))),
		Effects:   []plugin.Effect{{Kind: plugin.ContentEffect, Loss: plugin.Lossy, Detail: "audio.mix"}},
		Resources: resource.Request{Memory: resource.Bytes(planeBytes[float32](configuration.MaxSamples, channels))},
	}, nil
}

// mixerWeights pairs one level with each connected input. An empty list takes
// every input whole, which is the only shape that means the same thing however
// many of them there turn out to be.
func mixerWeights(stated []config.RatioValue, inputs int) ([]float32, error) {
	result := make([]float32, inputs)
	if len(stated) == 0 {
		for index := range result {
			result[index] = 1
		}
		return result, nil
	}
	if len(stated) != inputs {
		return nil, fmt.Errorf("%w: %d levels were given for %d inputs", ErrUnsupported, len(stated), inputs)
	}
	for index, value := range stated {
		result[index] = float32(value)
	}
	return result, nil
}

// mixedLength states how long the result lasts: as long as its longest input,
// because the shorter ones are taken for silence once they end rather than
// cutting the stream short. An input that states nothing leaves the answer
// unstated, since one of them may outlast everything the others said.
func mixedLength(properties property.Set, connected []stream.Descriptor) (property.Set, error) {
	var longest int64
	for _, descriptor := range connected {
		length, stated := stream.DurationOf(descriptor.Properties())
		if !stated {
			return stream.WithoutDuration(properties), nil
		}
		longest = max(longest, length.Value().Int64())
	}
	return stream.WithDuration(properties, timing.NewDuration(longest))
}
