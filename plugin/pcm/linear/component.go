package linear

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/audio"
	"github.com/godexture/godec/media/buffer"
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

var errMissingGrant = errors.New("linear PCM requires a payload buffer grant")

type componentPlan struct {
	operation operation
	shape     flow.Shape
	config    configuration
	// wire and frames are the descriptions Compile settled on. An operator
	// derives its pack or unpack loop from them once, at Open.
	wire   sample.Description
	frames sample.Description
}

// newFraming builds the operations that move payloads without interpreting
// samples, so one component serves every coding.
func newFraming[Marker any](kind operation, name string) plugin.Component {
	shape := framingShape(kind)
	options := []plugin.ComponentOption{
		plugin.WithSpec(newSpec(kind, shape, nil, openFraming)),
		framingExecution(kind),
	}
	if trait := operationFormat(kind); trait != nil {
		options = append(options, trait)
	}
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name}, configurationSchema(), options...)
}

// newCodec builds the decoder or encoder whose frames are stored as S. A port
// schema is static, so each canonical representation gets its own component
// rather than one component that changes shape with its configuration.
func newCodec[Marker any, S audio.Sample](kind operation, name string) plugin.Component {
	shape := codecShape[S](kind)
	spec := newSpec(kind, shape, sample.Stores[S], openCodec[S])
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name}, configurationSchema(),
		plugin.WithSpec(spec), codecExecution[S](kind))
}

func newSpec(kind operation, shape flow.Shape, stores func(sample.Coding) bool, open func(componentPlan, *buffer.Allocator) (flow.Operator, error)) plugin.Spec[configuration, componentPlan, stream.Descriptor] {
	return plugin.Spec[configuration, componentPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
			return compileOperation(kind, shape, stores, configuration, inputs)
		},
		Suggest: func(_ plugin.SuggestContext, suggestion plugin.Suggestion[stream.Descriptor]) []configuration {
			return suggestOperation(kind, shape, stores, suggestion)
		},
		SuggestionLimit: 1,
		Open: func(ctx plugin.OpenContext, plan componentPlan) (flow.Operator, error) {
			buffers := ctx.Buffers()
			if buffers == nil && operationResources(plan.operation, plan.config, plan.frames).Memory != 0 {
				return nil, errMissingGrant
			}
			return open(plan, buffers)
		},
	}
}

func suggestOperation(kind operation, shape flow.Shape, stores func(sample.Coding) bool, suggestion plugin.Suggestion[stream.Descriptor]) []configuration {
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
	if !ok || (stores != nil && !stores(value.Coding)) {
		return nil
	}
	value.Tag = codec.DemandedTag(suggestion).String()
	return []configuration{value}
}

func framingExecution(kind operation) plugin.ComponentOption {
	switch kind {
	case readerOperation:
		return plugin.WithProcessor("bytes", access.Bytes(), "chunks", format.Chunks())
	case parserOperation:
		return plugin.WithProcessor("chunks", format.Chunks(), "packets", codec.Packets())
	default:
		return plugin.WithProcessor("packets", codec.Packets(), "writes", access.Writes())
	}
}

func codecExecution[S audio.Sample](kind operation) plugin.ComponentOption {
	if kind == decoderOperation {
		return plugin.WithProcessor("packets", codec.Packets(), "frames", sample.Frames[S]())
	}
	return plugin.WithProcessor("frames", sample.Frames[S](), "packets", codec.Packets())
}

func framingShape(kind operation) flow.Shape {
	switch kind {
	case readerOperation:
		return flow.NewShape([]flow.Port{flow.In("bytes", access.Bytes())}, []flow.Port{flow.Out("chunks", format.Chunks())})
	case parserOperation:
		return flow.NewShape([]flow.Port{flow.In("chunks", format.Chunks())}, []flow.Port{flow.Out("packets", codec.Packets())})
	default:
		return flow.NewShape([]flow.Port{flow.In("packets", codec.Packets())}, []flow.Port{flow.Out("writes", access.Writes())})
	}
}

func codecShape[S audio.Sample](kind operation) flow.Shape {
	frames := sample.Frames[S]()
	if kind == decoderOperation {
		return flow.NewShape([]flow.Port{flow.In("packets", codec.Packets())}, []flow.Port{flow.Out("frames", frames)})
	}
	return flow.NewShape([]flow.Port{flow.In("frames", frames)}, []flow.Port{flow.Out("packets", codec.Packets())})
}

func compileOperation(kind operation, shape flow.Shape, stores func(sample.Coding) bool, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
	if stores != nil && !stores(configuration.Coding) {
		return plugin.Compiled[componentPlan, stream.Descriptor]{}, fmt.Errorf("%w: %s", ErrUnsupportedCoding, configuration.Coding)
	}
	inputPort, outputPort := shape.Inputs[0], shape.Outputs[0]
	input, ok := inputs.One(inputPort.ID())
	if !ok {
		return plugin.Compiled[componentPlan, stream.Descriptor]{
			Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require(inputPort.ID(), plugin.ConditionNeed[stream.Descriptor]("pcm.input"))},
		}, nil
	}

	actual, actualErr := sample.FromProperties(input.Properties())
	expected, output := operationDescriptions(kind, configuration, actual)
	if kind != readerOperation {
		if actualErr != nil || actual != expected || input.TimeBase() != timing.MustBase(1, int64(expected.Rate)) {
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
	if kind == encoderOperation && configuration.Tag != "" {
		properties, tagErr := codec.WithTag(outputDescriptor.Properties(), format.Tag(configuration.Tag))
		if tagErr != nil {
			return plugin.Compiled[componentPlan, stream.Descriptor]{}, tagErr
		}
		if outputDescriptor, err = stream.NewDescriptor(outputDescriptor.ID(), outputPort.Schema(), outputDescriptor.TimeBase(), properties); err != nil {
			return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
		}
		outputDescriptor = outputDescriptor.WithMetadata(input.Metadata())
	}
	plan := componentPlan{operation: kind, shape: shape.Clone(), config: configuration, wire: configuration.wire()}
	switch kind {
	case decoderOperation:
		plan.wire, plan.frames = expected, output
	case encoderOperation:
		plan.frames, plan.wire = expected, output
	}
	return plugin.Compiled[componentPlan, stream.Descriptor]{
		Plan:      plan,
		Outputs:   flow.NewDescriptors(flow.Describe(outputPort.ID(), outputDescriptor)),
		Effects:   []plugin.Effect{operationEffect(kind, expected, output)},
		Resources: operationResources(kind, configuration, plan.frames),
	}, nil
}

func operationResources(kind operation, configuration configuration, frames sample.Description) resource.Request {
	wire := configuration.wire()
	switch kind {
	case encoderOperation:
		return resource.Request{Memory: resource.Bytes(configuration.ChunkSamples * wire.BlockBytes())}
	case decoderOperation:
		planeBytes := configuration.ChunkSamples * frames.Coding.Bytes()
		bytes := planeBytes
		for channel := 1; channel < wire.Layout.Count(); channel++ {
			bytes = (bytes + 15) &^ 15
			bytes += planeBytes
		}
		return resource.Request{Memory: resource.Bytes(bytes)}
	default:
		return resource.Request{}
	}
}

// operationDescriptions returns the input this operation requires and the
// output it produces. An encoder consults the description it was handed: the
// wire carries whatever its coding can hold, so writing fewer valid bits than
// the frames offer is a reported loss rather than a mismatch to bridge.
func operationDescriptions(kind operation, configuration configuration, actual sample.Description) (sample.Description, sample.Description) {
	wire := configuration.wire()
	switch kind {
	case decoderOperation:
		return wire, wire.Decoded()
	case encoderOperation:
		expected := wire.Decoded()
		if actual.Valid() && actual.Coding == expected.Coding && actual.Packing == expected.Packing {
			expected.ValidBits = actual.ValidBits
			if actual.ValidBits < wire.ValidBits {
				wire.ValidBits = actual.ValidBits
			}
		}
		return expected, wire
	default:
		return wire, wire
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

func operationEffect(kind operation, expected, output sample.Description) plugin.Effect {
	switch kind {
	case decoderOperation:
		return plugin.Effect{Kind: plugin.RepresentationEffect, Loss: plugin.NoLoss, Detail: "pcm.decode"}
	case encoderOperation:
		loss := plugin.NoLoss
		if output.ValidBits < expected.ValidBits {
			loss = plugin.Lossy
		}
		return plugin.Effect{Kind: plugin.RepresentationEffect, Loss: loss, Detail: "pcm.encode"}
	default:
		return plugin.Effect{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "pcm.framing"}
	}
}

func descriptorWith(input stream.Descriptor, schemaDescriptor schema.Descriptor, description sample.Description) (stream.Descriptor, error) {
	properties, err := description.Apply(input.Properties())
	if err != nil {
		return stream.Descriptor{}, err
	}
	if description.Packing == sample.Planar {
		// A decoded stream is no longer the coded one, so it stops carrying the
		// tag that named the codec.
		properties = codec.WithoutTag(properties)
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
