package linear

import (
	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type operation uint8

const (
	readerOperation operation = iota + 1
	parserOperation
	decoderOperation
	encoderOperation
	writerOperation
)

type componentPlan struct {
	operation operation
	shape     flow.Shape
	config    configuration
}

func newComponent[Marker any](kind operation, name string) plugin.Component {
	shape := operationShape(kind)
	spec := plugin.Spec[configuration, componentPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
			return compileOperation(kind, shape, configuration, inputs)
		},
		Suggest: func(_ plugin.SuggestContext, suggestion plugin.Suggestion[stream.Descriptor]) []configuration {
			inputPort, outputPort := shape.Inputs[0], shape.Outputs[0]
			input, ok := suggestion.Inputs().One(inputPort.ID())
			if !ok {
				return nil
			}
			current, err := sample.FromProperties(input.Properties())
			if err != nil {
				return nil
			}
			var desired *sample.Description
			for _, demand := range suggestion.Demands() {
				if !wireSide(kind, demand.Port(), inputPort.ID(), outputPort.ID()) {
					continue
				}
				target, ok := demand.Need().Desired()
				if !ok {
					continue
				}
				value, err := sample.FromProperties(target.Properties())
				if err == nil {
					desired = &value
					break
				}
			}
			value, ok := suggestConfiguration(current, desired)
			if !ok {
				return nil
			}
			return []configuration{value}
		},
		SuggestionLimit: 1,
		Open: func(ctx plugin.OpenContext, plan componentPlan) (flow.Operator, error) {
			return openOperation(plan, ctx.Buffers())
		},
	}
	options := []plugin.ComponentOption{plugin.WithSpec(spec), operationExecution(kind)}
	if trait := operationFormat(kind); trait != nil {
		options = append(options, trait)
	}
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name}, configurationSchema(), options...)
}

func operationExecution(kind operation) plugin.ComponentOption {
	switch kind {
	case readerOperation:
		return plugin.WithProcessor("bytes", access.Bytes(), "chunks", format.Chunks())
	case parserOperation:
		return plugin.WithProcessor("chunks", format.Chunks(), "packets", codec.Packets())
	case decoderOperation:
		return plugin.WithProcessor("packets", codec.Packets(), "frames", sample.Frames[int16]())
	case encoderOperation:
		return plugin.WithProcessor("frames", sample.Frames[int16](), "packets", codec.Packets())
	case writerOperation:
		return plugin.WithProcessor("packets", codec.Packets(), "writes", access.Writes())
	default:
		return nil
	}
}

func operationShape(kind operation) flow.Shape {
	switch kind {
	case readerOperation:
		return flow.NewShape([]flow.Port{flow.In("bytes", access.Bytes())}, []flow.Port{flow.Out("chunks", format.Chunks())})
	case parserOperation:
		return flow.NewShape([]flow.Port{flow.In("chunks", format.Chunks())}, []flow.Port{flow.Out("packets", codec.Packets())})
	case decoderOperation:
		return flow.NewShape([]flow.Port{flow.In("packets", codec.Packets())}, []flow.Port{flow.Out("frames", sample.Frames[int16]())})
	case encoderOperation:
		return flow.NewShape([]flow.Port{flow.In("frames", sample.Frames[int16]())}, []flow.Port{flow.Out("packets", codec.Packets())})
	case writerOperation:
		return flow.NewShape([]flow.Port{flow.In("packets", codec.Packets())}, []flow.Port{flow.Out("writes", access.Writes())})
	default:
		return flow.Shape{}
	}
}

func compileOperation(kind operation, shape flow.Shape, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
	inputPort, outputPort := shape.Inputs[0], shape.Outputs[0]
	input, ok := inputs.One(inputPort.ID())
	if !ok {
		return plugin.Compiled[componentPlan, stream.Descriptor]{
			Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require(inputPort.ID(), plugin.ConditionNeed[stream.Descriptor]("pcm.input"))},
		}, nil
	}

	expected, output := operationDescriptions(kind, configuration)
	if kind != readerOperation {
		actual, err := sample.FromProperties(input.Properties())
		if err != nil || actual != expected || input.TimeBase() != timing.MustBase(1, int64(expected.Rate)) {
			desired, desiredErr := descriptorWith(input, inputPort.Schema(), expected)
			if desiredErr != nil {
				return plugin.Compiled[componentPlan, stream.Descriptor]{}, desiredErr
			}
			return plugin.Compiled[componentPlan, stream.Descriptor]{
				Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require(inputPort.ID(), plugin.DescriptorNeed("pcm.sample-description", desired))},
			}, nil
		}
	}

	var outputDescriptor stream.Descriptor
	var err error
	switch kind {
	case readerOperation:
		outputDescriptor, err = describedCarrier(input, outputPort.Schema(), output)
	case writerOperation:
		outputDescriptor, err = carrierDescriptor(input, outputPort.Schema())
	default:
		outputDescriptor, err = descriptorWith(input, outputPort.Schema(), output)
	}
	if err != nil {
		return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
	}
	return plugin.Compiled[componentPlan, stream.Descriptor]{
		Plan:      componentPlan{operation: kind, shape: shape.Clone(), config: configuration},
		Outputs:   flow.NewDescriptors(flow.Describe(outputPort.ID(), outputDescriptor)),
		Effects:   []plugin.Effect{operationEffect(kind)},
		Resources: operationResources(kind, configuration),
	}, nil
}

func operationResources(kind operation, configuration configuration) resource.Request {
	channels := configuration.Layout.Count()
	planeBytes := configuration.ChunkSamples * 2
	interleavedBytes := planeBytes * channels
	switch kind {
	case encoderOperation:
		return resource.Request{Memory: resource.Bytes(interleavedBytes)}
	case decoderOperation:
		bytes := planeBytes
		for channel := 1; channel < channels; channel++ {
			bytes = (bytes + 15) &^ 15
			bytes += planeBytes
		}
		return resource.Request{Memory: resource.Bytes(bytes)}
	default:
		return resource.Request{}
	}
}

func operationDescriptions(kind operation, configuration configuration) (sample.Description, sample.Description) {
	switch kind {
	case decoderOperation:
		return configuration.wire(), configuration.planar()
	case encoderOperation:
		return configuration.planar(), configuration.wire()
	default:
		return configuration.wire(), configuration.wire()
	}
}

// wireSide reports whether a demand on this port describes the interleaved wire
// samples this operation reads or writes. A decoder emits planar frames and an
// encoder consumes them, so a demand on that side says nothing about the byte
// order of the wire and must not configure it.
func wireSide(kind operation, port, input, output string) bool {
	switch kind {
	case decoderOperation:
		return port == input
	case encoderOperation:
		return port == output
	default:
		return port == input || port == output
	}
}

func operationEffect(kind operation) plugin.Effect {
	if kind == decoderOperation || kind == encoderOperation {
		return plugin.Effect{Kind: plugin.RepresentationEffect, Loss: plugin.NoLoss, Detail: "pcm.s16-representation"}
	}
	return plugin.Effect{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "pcm.framing"}
}

func descriptorWith(input stream.Descriptor, schemaDescriptor schema.Descriptor, description sample.Description) (stream.Descriptor, error) {
	properties, err := description.Apply(input.Properties())
	if err != nil {
		return stream.Descriptor{}, err
	}
	result, err := stream.NewDescriptor(input.ID(), schemaDescriptor, timing.MustBase(1, int64(description.Rate)), properties)
	if err != nil {
		return stream.Descriptor{}, err
	}
	return result.WithMetadata(input.Metadata()), nil
}

func describedCarrier(input stream.Descriptor, schemaDescriptor schema.Descriptor, description sample.Description) (stream.Descriptor, error) {
	properties, err := description.Properties()
	if err != nil {
		return stream.Descriptor{}, err
	}
	result, err := stream.NewDescriptor(input.ID(), schemaDescriptor, timing.MustBase(1, int64(description.Rate)), properties)
	if err != nil {
		return stream.Descriptor{}, err
	}
	return result.WithMetadata(input.Metadata()), nil
}

func carrierDescriptor(input stream.Descriptor, schemaDescriptor schema.Descriptor) (stream.Descriptor, error) {
	result, err := stream.NewDescriptor(input.ID(), schemaDescriptor, timing.Base{}, property.New())
	if err != nil {
		return stream.Descriptor{}, err
	}
	return result.WithMetadata(input.Metadata()), nil
}
