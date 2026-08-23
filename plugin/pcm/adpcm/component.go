package adpcm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/pcm/internal/adpcm/param"
	"github.com/godexture/godec/resource"
)

var (
	ErrParameters   = errors.New("ADPCM stream does not carry the parameters its blocks need")
	ErrPartialBlock = errors.New("ADPCM payload ends inside a block")
)

type componentPlan struct {
	operation  operation
	shape      flow.Shape
	variant    Variant
	parameters param.Parameters
	channels   int
	blocks     int
}

type operation uint8

const (
	parserOperation operation = iota + 1
	decoderOperation
)

type configID struct{}

// configuration bounds how many blocks one item may hold. Everything else a
// block needs is stated by the stream: the container carries the parameters
// and this family reads them.
type configuration struct {
	ChunkBlocks int
}

func configurationSchema() config.Schema[configuration] {
	return config.Struct[configID](func() configuration { return configuration{ChunkBlocks: 16} }).
		Version("1").
		AddField(config.Field("chunkBlocks", func(value *configuration) *int { return &value.ChunkBlocks }, config.Int().Range(1, 1<<16))).
		Build()
}

// parametersOf reads the block layout from the codec extension the container
// carried. Deriving samples per block from the block size is what the standard
// layouts do, and a stream that states a different number is rejected rather
// than decoded against the wrong geometry.
func parametersOf(variant Variant, input stream.Descriptor, channels int) (param.Parameters, error) {
	carried, ok := codec.ParametersOf(input.Properties())
	if !ok {
		return param.Parameters{}, ErrParameters
	}
	extension := carried.AppendTo(nil)
	if len(extension) < 2 {
		return param.Parameters{}, fmt.Errorf("%w: extension holds %d bytes", ErrParameters, len(extension))
	}
	result := param.Parameters{SamplesPerBlock: binary.LittleEndian.Uint16(extension[0:2])}
	if variant == Microsoft {
		if len(extension) < 4 {
			return param.Parameters{}, fmt.Errorf("%w: no coefficient count", ErrParameters)
		}
		count := int(binary.LittleEndian.Uint16(extension[2:4]))
		if len(extension) < 4+count*4 {
			return param.Parameters{}, fmt.Errorf("%w: %d coefficients do not fit in %d bytes", ErrParameters, count, len(extension))
		}
		result.Coefficients = make([]param.Coefficient, count)
		for index := range result.Coefficients {
			offset := 4 + index*4
			result.Coefficients[index] = param.Coefficient{
				Coeff1: int16(binary.LittleEndian.Uint16(extension[offset : offset+2])),
				Coeff2: int16(binary.LittleEndian.Uint16(extension[offset+2 : offset+4])),
			}
		}
	}
	align, err := blockAlign(variant, channels, int(result.SamplesPerBlock))
	if err != nil {
		return param.Parameters{}, err
	}
	result.BlockAlign = uint16(align)
	if err := result.Validate(variant.kind(), channels); err != nil {
		return param.Parameters{}, fmt.Errorf("%w: %w", ErrParameters, err)
	}
	return result, nil
}

// blockAlign inverts the samples-per-block rule each layout states, which is
// how a decoder recovers the block size from the parameters alone.
func blockAlign(variant Variant, channels, samples int) (int, error) {
	switch {
	case samples <= 0:
		return 0, fmt.Errorf("%w: a block holds %d samples", ErrParameters, samples)
	case variant == Microsoft && channels == 1:
		return (samples-2)/2 + 7, nil
	case variant == Microsoft && channels == 2:
		return samples + 12, nil
	case variant == IMA && channels == 1:
		return (samples-1)/2 + 4, nil
	case variant == IMA && channels == 2:
		return samples + 7, nil
	default:
		return 0, fmt.Errorf("%w: %s does not describe %d channels", ErrParameters, variant, channels)
	}
}

func newParser[Marker any](variant Variant, name string) plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("chunks", mediaformat.Chunks())},
		[]flow.Port{flow.Out("packets", codec.Packets())},
	)
	spec := plugin.Spec[configuration, componentPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
			input, parameters, channels, ready, err := compileInput(variant, shape, inputs)
			if !ready || err != nil {
				return incomplete(shape, err)
			}
			output, err := stream.NewDescriptor(input.ID(), codec.Packets().Descriptor(), input.TimeBase(), input.Properties())
			if err != nil {
				return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[componentPlan, stream.Descriptor]{
				Plan: componentPlan{
					operation: parserOperation, shape: shape.Clone(), variant: variant,
					parameters: parameters, channels: channels, blocks: configuration.ChunkBlocks,
				},
				Outputs: flow.NewDescriptors(flow.Describe("packets", output.WithMetadata(input.Metadata()))),
				Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "adpcm.framing"}},
			}, nil
		},
		Open: func(_ plugin.OpenContext, plan componentPlan) (flow.Operator, error) {
			return &parserOperator{operatorBase: operatorBase{shape: plan.shape.Clone()}, parameters: plan.parameters}, nil
		},
	}
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("chunks", mediaformat.Chunks(), "packets", codec.Packets()))
}

func newDecoder[Marker any](variant Variant, name string) plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("packets", codec.Packets())},
		[]flow.Port{flow.Out("frames", sample.Frames[int16]())},
	)
	spec := plugin.Spec[configuration, componentPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, configuration configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
			input, parameters, channels, ready, err := compileInput(variant, shape, inputs)
			if !ready || err != nil {
				return incomplete(shape, err)
			}
			signal, err := sample.SignalOf(input.Properties())
			if err != nil {
				return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
			}
			// Expansion recovers a value spanning the whole 16-bit container,
			// so the decoded stream states that depth rather than the four
			// bits one coded sample occupies.
			decoded := sample.Description{
				Signal:  sample.Signal{Rate: signal.Rate, Layout: signal.Layout, ValidBits: 16},
				Coding:  sample.S16,
				Packing: sample.Planar,
				Endian:  sample.NoEndian,
			}
			properties, err := decoded.Apply(input.Properties())
			if err != nil {
				return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
			}
			properties = codec.WithoutParameters(properties.Delete(codec.Tag().ID()))
			output, err := stream.NewDescriptor(input.ID(), shape.Outputs[0].Schema(), input.TimeBase(), properties)
			if err != nil {
				return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[componentPlan, stream.Descriptor]{
				Plan: componentPlan{
					operation: decoderOperation, shape: shape.Clone(), variant: variant,
					parameters: parameters, channels: channels, blocks: configuration.ChunkBlocks,
				},
				Outputs: flow.NewDescriptors(flow.Describe("frames", output.WithMetadata(input.Metadata()))),
				Effects: []plugin.Effect{{Kind: plugin.RepresentationEffect, Loss: plugin.NoLoss, Detail: "adpcm.expand"}},
				Resources: resource.Request{
					Memory: resource.Bytes(configuration.ChunkBlocks * int(parameters.SamplesPerBlock) * 2 * channels),
				},
			}, nil
		},
		Open: func(ctx plugin.OpenContext, plan componentPlan) (flow.Operator, error) {
			if ctx.Buffers() == nil {
				return nil, errors.New("ADPCM requires a payload buffer grant")
			}
			return newDecoderOperator(plan, ctx.Buffers()), nil
		},
	}
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: name}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("packets", codec.Packets(), "frames", sample.Frames[int16]()))
}

func compileInput(variant Variant, shape flow.Shape, inputs flow.Descriptors[stream.Descriptor]) (stream.Descriptor, param.Parameters, int, bool, error) {
	input, ok := inputs.One(shape.Inputs[0].ID())
	if !ok {
		return stream.Descriptor{}, param.Parameters{}, 0, false, nil
	}
	signal, err := sample.SignalOf(input.Properties())
	if err != nil {
		return stream.Descriptor{}, param.Parameters{}, 0, false, err
	}
	channels := signal.Layout.Count()
	parameters, err := parametersOf(variant, input, channels)
	if err != nil {
		return stream.Descriptor{}, param.Parameters{}, 0, false, err
	}
	return input, parameters, channels, true, nil
}

func incomplete(shape flow.Shape, err error) (plugin.Compiled[componentPlan, stream.Descriptor], error) {
	if err != nil {
		return plugin.Compiled[componentPlan, stream.Descriptor]{}, err
	}
	return plugin.Compiled[componentPlan, stream.Descriptor]{
		Requirements: []plugin.Requirement[stream.Descriptor]{
			plugin.Require(shape.Inputs[0].ID(), plugin.ConditionNeed[stream.Descriptor]("adpcm.input")),
		},
	}, nil
}
