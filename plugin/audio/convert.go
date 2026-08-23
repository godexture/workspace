package audio

import (
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	mediaaudio "github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type configID struct{}

type configuration struct {
	MaxSamples int
}

func defaultConfiguration() configuration { return configuration{MaxSamples: 8192} }

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](defaultConfiguration).
		Version("1").
		AddField(config.Field("maxSamples", func(value *configuration) *int { return &value.MaxSamples }, config.Int().Range(1, 1<<20).
			Help("largest frame this converter reserves output planes for"))).
		Build()
}

type convertPlan struct {
	shape    flow.Shape
	channels int
	samples  int
}

func newComponent[Marker any, From, To mediaaudio.Sample]() plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("frames", sample.Frames[From]())},
		[]flow.Port{flow.Out("converted", sample.Frames[To]())},
	)
	spec := plugin.Spec[configuration, convertPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[convertPlan, stream.Descriptor], error) {
			return compile[From, To](shape, configuration, inputs)
		},
		Open: func(ctx plugin.OpenContext, plan convertPlan) (flow.Operator, error) {
			if ctx.Buffers() == nil {
				return nil, fmt.Errorf("%w: sample conversion requires a payload buffer grant", ErrUnsupported)
			}
			return newOperator[From, To](plan, ctx.Buffers()), nil
		},
	}
	name := fmt.Sprintf("Audio %s to %s conversion", sample.CodingOf[From](), sample.CodingOf[To]())
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("frames", sample.Frames[From](), "converted", sample.Frames[To]()),
	)
}

func compile[From, To mediaaudio.Sample](shape flow.Shape, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[convertPlan, stream.Descriptor], error) {
	input, ok := inputs.One("frames")
	if !ok {
		return plugin.Compiled[convertPlan, stream.Descriptor]{
			Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("frames", plugin.ConditionNeed[stream.Descriptor]("audio.conversion-input"))},
		}, nil
	}
	description, err := sample.FromProperties(input.Properties())
	if err != nil || description.Packing != sample.Planar || description.Coding != sample.CodingOf[From]() {
		return plugin.Compiled[convertPlan, stream.Descriptor]{}, fmt.Errorf("%w: sample conversion reads %s planar frames", ErrUnsupported, sample.CodingOf[From]())
	}
	output := converted[To](description)
	properties, err := output.Apply(input.Properties())
	if err != nil {
		return plugin.Compiled[convertPlan, stream.Descriptor]{}, err
	}
	descriptor, err := stream.NewDescriptor(input.ID(), shape.Outputs[0].Schema(), input.TimeBase(), properties)
	if err != nil {
		return plugin.Compiled[convertPlan, stream.Descriptor]{}, err
	}
	channels := description.Layout.Count()
	return plugin.Compiled[convertPlan, stream.Descriptor]{
		Plan:      convertPlan{shape: shape.Clone(), channels: channels, samples: configuration.MaxSamples},
		Outputs:   flow.NewDescriptors(flow.Describe("converted", descriptor.WithMetadata(input.Metadata()))),
		Effects:   []plugin.Effect{effect(description, output)},
		Resources: resource.Request{Memory: resource.Bytes(planeBytes[To](configuration.MaxSamples, channels))},
	}, nil
}

// converted is the description the same stream has once its samples are stored
// as To. Valid bits describe the signal, so widening keeps them and narrowing
// caps them at what the new representation can hold.
func converted[To mediaaudio.Sample](input sample.Description) sample.Description {
	result := input
	result.Coding = sample.CodingOf[To]()
	result.ValidBits = min(input.ValidBits, result.Coding.Bits())
	if result.Coding.Float() {
		result.ValidBits = result.Coding.Bits()
	}
	return result
}

// effect reports whether the conversion can restore the signal it received.
// A float target holds an integer source exactly while its mantissa is wide
// enough; past that, and whenever the target carries fewer valid bits, the
// conversion rounds.
func effect(input, output sample.Description) plugin.Effect {
	loss := plugin.NoLoss
	switch {
	case output.Coding == sample.F32 && input.ValidBits > 24:
		loss = plugin.Lossy
	case output.Coding == sample.F64 && input.ValidBits > 53:
		loss = plugin.Lossy
	case !output.Coding.Float() && output.ValidBits < input.ValidBits:
		loss = plugin.Lossy
	}
	return plugin.Effect{Kind: plugin.RepresentationEffect, Loss: loss, Detail: "audio.sample-conversion"}
}

func planeBytes[S mediaaudio.Sample](samples, channels int) int {
	plane := samples * sampleSize[S]()
	total := plane
	for channel := 1; channel < channels; channel++ {
		total = (total+15)&^15 + plane
	}
	return total
}
