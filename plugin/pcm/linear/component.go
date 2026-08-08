package linear

import (
	"github.com/godexture/godec/flow"
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
		Shape: plugin.StaticShape[configuration](shape),
		Compile: func(_ plugin.CompileContext, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
			return compileOperation(kind, shape, configuration, inputs)
		},
		Suggest: func(_ plugin.SuggestContext, input stream.Descriptor, need plugin.Need[stream.Descriptor]) []configuration {
			current, err := sample.FromProperties(input.Properties())
			if err != nil {
				return nil
			}
			var desired *sample.Description
			if target, ok := need.Desired(); ok {
				value, err := sample.FromProperties(target.Properties())
				if err == nil {
					desired = &value
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
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name}, configurationSchema(), plugin.WithSpec(spec), operationExecution(kind))
}

func operationExecution(kind operation) plugin.ComponentOption {
	switch kind {
	case readerOperation:
		return plugin.WithProcessor("bytes", Bytes(), "chunks", Chunks())
	case parserOperation:
		return plugin.WithProcessor("chunks", Chunks(), "packets", Packets())
	case decoderOperation:
		return plugin.WithProcessor("packets", Packets(), "frames", sample.S16())
	case encoderOperation:
		return plugin.WithProcessor("frames", sample.S16(), "packets", Packets())
	case writerOperation:
		return plugin.WithProcessor("packets", Packets(), "bytes", Bytes())
	default:
		return nil
	}
}

func operationShape(kind operation) flow.Shape {
	switch kind {
	case readerOperation:
		return flow.NewShape([]flow.Port{flow.In("bytes", Bytes())}, []flow.Port{flow.Out("chunks", Chunks())})
	case parserOperation:
		return flow.NewShape([]flow.Port{flow.In("chunks", Chunks())}, []flow.Port{flow.Out("packets", Packets())})
	case decoderOperation:
		return flow.NewShape([]flow.Port{flow.In("packets", Packets())}, []flow.Port{flow.Out("frames", sample.S16())})
	case encoderOperation:
		return flow.NewShape([]flow.Port{flow.In("frames", sample.S16())}, []flow.Port{flow.Out("packets", Packets())})
	case writerOperation:
		return flow.NewShape([]flow.Port{flow.In("packets", Packets())}, []flow.Port{flow.Out("bytes", Bytes())})
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
	actual, err := sample.FromProperties(input.Properties())
	if err != nil || actual != expected || input.TimeBase() != timing.MustBase(1, int64(expected.Rate)) {
		desired, desiredErr := descriptorWith(input, inputPort.Schema().Identity(), expected)
		if desiredErr != nil {
			return plugin.Compiled[componentPlan, stream.Descriptor]{}, desiredErr
		}
		return plugin.Compiled[componentPlan, stream.Descriptor]{
			Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require(inputPort.ID(), plugin.DescriptorNeed("pcm.sample-description", desired))},
		}, nil
	}

	outputDescriptor, err := descriptorWith(input, outputPort.Schema().Identity(), output)
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
	channels := configuration.Layout.Channels()
	planeBytes := configuration.ChunkSamples * 2
	interleavedBytes := planeBytes * channels
	switch kind {
	case readerOperation, encoderOperation:
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

func operationEffect(kind operation) plugin.Effect {
	if kind == decoderOperation || kind == encoderOperation {
		return plugin.Effect{Kind: plugin.RepresentationEffect, Loss: plugin.NoLoss, Detail: "pcm.s16-representation"}
	}
	return plugin.Effect{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "pcm.framing"}
}

func descriptorWith(input stream.Descriptor, schemaID schema.ID, description sample.Description) (stream.Descriptor, error) {
	properties, err := description.Apply(input.Properties())
	if err != nil {
		return stream.Descriptor{}, err
	}
	result, err := stream.NewDescriptor(input.ID(), schemaID, timing.MustBase(1, int64(description.Rate)), properties)
	if err != nil {
		return stream.Descriptor{}, err
	}
	return result.WithMetadata(input.Metadata()), nil
}
