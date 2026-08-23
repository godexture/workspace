package g711

import (
	"errors"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type operation uint8

const (
	parserOperation operation = iota + 1
	decoderOperation
	encoderOperation
)

var (
	ErrPartialSample = errors.New("companded payload ends inside a sample frame")
	ErrPlaneCount    = errors.New("companded frame plane count does not match its channel layout")
)

type componentPlan struct {
	operation operation
	shape     flow.Shape
	law       Law
	channels  int
	samples   int
}

type configID struct{}

// configuration carries only what a companded stream cannot state for itself.
// The companding law belongs to the component, and the rate and layout come
// from the stream, so the one setting left is how much a frame may hold.
type configuration struct {
	ChunkSamples int
	// Tag is the codec tag the output carries. A coder does not know what its
	// container calls it, so the container states it in the stream it asks for.
	Tag string
}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](func() configuration { return configuration{ChunkSamples: 1024} }).
		Version("1").
		AddField(config.Field("chunkSamples", func(value *configuration) *int { return &value.ChunkSamples }, config.Int().Range(1, 1<<20))).
		AddField(config.Field("tag", func(value *configuration) *string { return &value.Tag }, config.String())).
		Build()
}

func newParser[Marker any]() plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("chunks", mediaformat.Chunks())},
		[]flow.Port{flow.Out("packets", codec.Packets())},
	)
	spec := plugin.Spec[configuration, componentPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
			input, signal, ready, err := compileInput(shape, inputs)
			if !ready || err != nil {
				return incomplete(shape, err)
			}
			output, err := stream.NewDescriptor(input.ID(), codec.Packets().Descriptor(), input.TimeBase(), input.Properties())
			if err != nil {
				return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[componentPlan, stream.Descriptor]{
				Plan:    componentPlan{operation: parserOperation, shape: shape.Clone(), channels: signal.Layout.Count()},
				Outputs: flow.NewDescriptors(flow.Describe("packets", output.WithMetadata(input.Metadata()))),
				Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "g711.framing"}},
			}, nil
		},
		Open: func(_ plugin.OpenContext, plan componentPlan) (flow.Operator, error) {
			return &parserOperator{operatorBase: operatorBase{shape: plan.shape.Clone()}, channels: plan.channels}, nil
		},
	}
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: "Companded parser"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("chunks", mediaformat.Chunks(), "packets", codec.Packets()))
}

func newCodec[Marker any](law Law, kind operation, name string) plugin.Component {
	shape := codecShape(kind)
	spec := plugin.Spec[configuration, componentPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
			return compileCodec(law, kind, shape, configuration, inputs)
		},
		Suggest: func(_ plugin.SuggestContext, suggestion plugin.Suggestion[stream.Descriptor]) []configuration {
			return []configuration{{ChunkSamples: 1024, Tag: codec.DemandedTag(suggestion).String()}}
		},
		SuggestionLimit: 1,
		Open: func(ctx plugin.OpenContext, plan componentPlan) (flow.Operator, error) {
			if ctx.Buffers() == nil {
				return nil, errors.New("G.711 requires a payload buffer grant")
			}
			return openCodec(plan, ctx.Buffers())
		},
	}
	execution := plugin.WithProcessor("packets", codec.Packets(), "frames", sample.Frames[int16]())
	if kind == encoderOperation {
		execution = plugin.WithProcessor("frames", sample.Frames[int16](), "packets", codec.Packets())
	}
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name}, configurationSchema(),
		plugin.WithSpec(spec), execution)
}

func codecShape(kind operation) flow.Shape {
	frames := sample.Frames[int16]()
	if kind == decoderOperation {
		return flow.NewShape([]flow.Port{flow.In("packets", codec.Packets())}, []flow.Port{flow.Out("frames", frames)})
	}
	return flow.NewShape([]flow.Port{flow.In("frames", frames)}, []flow.Port{flow.Out("packets", codec.Packets())})
}

// compileInput reads the signal every companded stream states. A stream that
// states none is not one this family can frame or decode.
func compileInput(shape flow.Shape, inputs flow.Descriptors[stream.Descriptor]) (stream.Descriptor, sample.Signal, bool, error) {
	input, ok := inputs.One(shape.Inputs[0].ID())
	if !ok {
		return stream.Descriptor{}, sample.Signal{}, false, nil
	}
	signal, err := sample.SignalOf(input.Properties())
	if err != nil {
		return stream.Descriptor{}, sample.Signal{}, false, err
	}
	return input, signal, true, nil
}

func incomplete(shape flow.Shape, err error) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
	if err != nil {
		return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
	}
	return plugin.Compiled[componentPlan, stream.Descriptor]{
		Requirements: []plugin.Requirement[stream.Descriptor]{
			plugin.Require(shape.Inputs[0].ID(), plugin.ConditionNeed[stream.Descriptor]("g711.input")),
		},
	}, nil
}

func compileCodec(law Law, kind operation, shape flow.Shape, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
	input, signal, ready, err := compileInput(shape, inputs)
	if !ready || err != nil {
		return incomplete(shape, err)
	}
	channels := signal.Layout.Count()
	plan := componentPlan{operation: kind, shape: shape.Clone(), law: law, channels: channels, samples: configuration.ChunkSamples}
	properties, memory, err := codecOutput(kind, input, signal, channels, configuration.ChunkSamples)
	if err == nil && kind == encoderOperation && configuration.Tag != "" {
		properties, err = codec.WithTag(properties, mediaformat.Tag(configuration.Tag))
	}
	if err != nil {
		return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
	}
	output, err := stream.NewDescriptor(input.ID(), shape.Outputs[0].Schema(), input.TimeBase(), properties)
	if err != nil {
		return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
	}
	return plugin.Compiled[componentPlan, stream.Descriptor]{
		Plan:      plan,
		Outputs:   flow.NewDescriptors(flow.Describe(shape.Outputs[0].ID(), output.WithMetadata(input.Metadata()))),
		Effects:   []plugin.Effect{codecEffect(kind)},
		Resources: resource.Request{Memory: resource.Bytes(memory)},
	}, nil
}

// codecOutput describes the stream on the far side of the codec. Expansion
// recovers a value spanning the whole 16-bit container, so a decoded stream
// states that depth rather than the width of the byte the header declared.
func codecOutput(kind operation, input stream.Descriptor, signal sample.Signal, channels, chunk int) (property.Set, int, error) {
	if kind == decoderOperation {
		decoded := sample.Description{
			Signal:  sample.Signal{Rate: signal.Rate, Layout: signal.Layout, ValidBits: 16},
			Coding:  sample.S16,
			Packing: sample.Planar,
			Endian:  sample.NoEndian,
		}
		properties, err := decoded.Apply(input.Properties())
		if err != nil {
			return property.Set{}, 0, err
		}
		// A decoded stream is no longer the coded one, so it stops carrying
		// the tag that named the codec.
		return properties.Delete(codec.Tag().ID()), chunk * 2 * channels, nil
	}
	description, err := sample.FromProperties(input.Properties())
	if err != nil || description.Coding != sample.S16 || description.Packing != sample.Planar {
		return property.Set{}, 0, errors.New("G.711 encodes signed 16-bit planar frames")
	}
	properties, err := sample.Signal{Rate: signal.Rate, Layout: signal.Layout}.Properties()
	if err != nil {
		return property.Set{}, 0, err
	}
	return properties, chunk * channels, nil
}

func codecEffect(kind operation) plugin.Effect {
	if kind == decoderOperation {
		return plugin.Effect{Kind: plugin.RepresentationEffect, Loss: plugin.NoLoss, Detail: "g711.expand"}
	}
	return plugin.Effect{Kind: plugin.CompressionEffect, Loss: plugin.Lossy, Detail: "g711.compand"}
}
