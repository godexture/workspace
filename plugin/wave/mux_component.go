package wave

import (
	"errors"
	"fmt"
	"math"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type muxPlan struct {
	shape  flow.Shape
	header muxHeader
	// rewrite is semantic metadata only. The source carrier is read at Open
	// and rewritten into a bounded payload there; no source bytes or handle
	// are retained by Compile.
	rewrite       metadata.Document
	rewriteNeeded bool
}

func muxerShape() flow.Shape {
	return flow.NewShape(
		[]flow.Port{flow.In("packets", codec.Packets())},
		[]flow.Port{flow.Out("writes", access.Writes())},
	)
}

func muxerComponent() plugin.Component {
	shape := muxerShape()
	spec := plugin.Spec[muxConfiguration, muxPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(ctx plugin.CompileContext, configuration muxConfiguration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[muxPlan, stream.Descriptor], error) {
			input, ok := inputs.One("packets")
			if !ok {
				return plugin.Compiled[muxPlan, stream.Descriptor]{
					Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("packets", plugin.ConditionNeed[stream.Descriptor]("wave.input"))},
				}, nil
			}
			// Every WAVE stream states a signal. Only one whose samples are
			// stored one scalar each also states a description, and only that
			// one can be rewritten into another representation.
			signal, err := sample.SignalOf(input.Properties())
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, diagnostic.NewError(diagnostic.NewItem(
					"wave.signal", diagnostic.ErrorSeverity, diagnostic.Path{}, "WAVE muxer requires a sample rate and channel layout", nil,
				))
			}
			outputCodec, description, requirement, err := muxCodec(input, signal, configuration.Codec)
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
			}
			if requirement.Valid() {
				return plugin.Compiled[muxPlan, stream.Descriptor]{
					Requirements: []plugin.Requirement[stream.Descriptor]{requirement},
				}, nil
			}
			if description.Valid() {
				signal = description.Signal
			}
			var metadataLost []metadata.Loss
			var muxHeaderValue muxHeader
			var rewrite metadata.Document
			var rewriteNeeded bool
			inspected, handedOff := mediaformat.InspectionOf[header](ctx, WAVE())
			if handedOff {
				if !inspected.valid() {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, fmt.Errorf("%w: WAVE mux received an invalid inspection", ErrMalformed)
				}
				if inspected.ranges.any() && (description != inspected.description || signal != inspected.signal) {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, fmt.Errorf("%w: WAVE source ranges cannot be reused after changing the stream", ErrUnsupported)
				}
				if len(input.Metadata().Blocks()) != 0 {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, fmt.Errorf("%w: WAVE mux metadata contains source blobs without a range inspection handoff", ErrUnsupported)
				}
				geometry, err := muxGeometry(outputCodec, signal, input, inspected)
				if err != nil {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
				}
				muxHeaderValue, err = newRangeMuxHeader(outputCodec, signal, geometry, !inspected.ranges.any() && outputCodec.coding == "", inspected)
				if err != nil {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
				}
				if !sameSemanticDocument(input.Metadata(), inspected.metadata) {
					if inspected.ranges.infoCount != 1 || !inspected.ranges.info.valid() {
						return plugin.Compiled[muxPlan, stream.Descriptor]{}, fmt.Errorf("%w: WAVE semantic metadata changed but no bounded LIST/INFO rewrite is available", ErrUnsupported)
					}
					if inspected.ranges.info.length > waveSemanticCap || !semanticWithinCap(input.Metadata(), waveSemanticCap) {
						return plugin.Compiled[muxPlan, stream.Descriptor]{}, fmt.Errorf("%w: WAVE semantic metadata exceeds the bounded rewrite cap", ErrUnsupported)
					}
					if err := validateSemanticInfoDocument(input.Metadata()); err != nil {
						return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
					}
					rewrite = input.Metadata()
					rewriteNeeded = true
				}
			} else {
				// A writer with no source inspection is a new WAVE output. Keep
				// this bounded construction path for standalone component use;
				// source-derived metadata must arrive through the handoff above.
				for _, block := range input.Metadata().Blocks() {
					if _, ok := parseChunkBlockID(block.ID()); ok {
						return plugin.Compiled[muxPlan, stream.Descriptor]{}, fmt.Errorf("%w: WAVE mux requires the same-format inspection handoff for source metadata", ErrUnsupported)
					}
				}
				resolver, _ := metadata.ResolverOf(ctx)
				chunks, err := marshalMuxChunks(ctx.Context(), resolver, input.Metadata())
				if err != nil {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
				}
				metadataLost = chunks.lost
				geometry, err := muxGeometry(outputCodec, signal, input, header{})
				if err != nil {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
				}
				muxHeaderValue, err = newMuxHeaderWithChunks(outputCodec, signal, geometry, outputCodec.coding == "", chunks)
				if err != nil {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
				}
			}
			output, err := stream.NewDescriptor(input.ID(), access.Writes().Descriptor(), timing.Base{}, property.New())
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[muxPlan, stream.Descriptor]{
				Plan:    muxPlan{shape: shape.Clone(), header: muxHeaderValue, rewrite: rewrite, rewriteNeeded: rewriteNeeded},
				Outputs: flow.NewDescriptors(flow.Describe("writes", output.WithMetadata(input.Metadata()))),
				Effects: append([]plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "wave-mux"}},
					metadataLossEffects(metadataLost)...),
				Resources: resource.Request{Memory: resource.Bytes(func() int {
					if muxHeaderValue.rangeMode {
						return max(muxHeaderValue.payloadBytes(), wavePageSize)
					}
					return muxHeaderValue.payloadBytes()
				}())},
			}, nil
		},
		Open: func(ctx plugin.OpenContext, plan muxPlan) (flow.Operator, error) {
			if len(plan.header.initial) == 0 {
				return nil, errors.New("WAVE mux plan is invalid")
			}
			if ctx.Buffers() == nil {
				return nil, errors.New("WAVE muxer requires a payload buffer grant")
			}
			operator := newMuxer(plan, ctx.Buffers())
			if plan.header.rangeMode && plan.header.ranges.any() {
				opening, ok := mediaformat.SourceOpening(ctx)
				if !ok {
					return nil, errors.New("WAVE range mux requires the inspected source opening")
				}
				if err := operator.setSource(ctx.Context(), opening); err != nil {
					return nil, err
				}
				if plan.rewriteNeeded {
					if plan.header.ranges.info.length > waveSemanticCap || plan.header.ranges.info.offset > uint64(^uint64(0)>>1) {
						return nil, fmt.Errorf("%w: WAVE LIST/INFO rewrite exceeds runtime limits", ErrUnsupported)
					}
					raw := make([]byte, int(plan.header.ranges.info.length))
					if err := access.ReadFullAt(ctx.Context(), operator.source, raw, int64(plan.header.ranges.info.offset)); err != nil {
						return nil, fmt.Errorf("%w: WAVE LIST/INFO rewrite source: %w", ErrTruncatedData, err)
					}
					payload, err := rewriteInfoSource(raw, plan.rewrite)
					if err != nil {
						return nil, err
					}
					if uint64(len(payload)) > waveSemanticCap {
						return nil, fmt.Errorf("%w: WAVE LIST/INFO rewrite exceeds the bounded cap", ErrUnsupported)
					}
					if err := operator.header.applyReplacement(sourceReplacement{source: plan.header.ranges.info, payload: payload}); err != nil {
						return nil, err
					}
				}
			}
			return operator, nil
		},
	}
	return plugin.NewComponent[muxerID](plugin.Descriptor{DisplayName: "WAVE muxer"}, muxConfigurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("packets", codec.Packets(), "writes", access.Writes()),
		mediaformat.Write(WAVE(), access.NewRequirements(access.AllOf(access.RandomWrite))),
	)
}

// muxDescription is the closest description this muxer can write for the one
// it received. WAVE stores interleaved little-endian samples, and a coding it
// has no format tag for falls back to signed 16-bit. An unrepresentable rate
// or channel layout is left unchanged so the header marshaller reports it.
func muxDescription(value sample.Description, requested sample.Coding) sample.Description {
	result := value
	result.Packing = sample.Interleaved
	if requested.Valid() {
		result.Coding = requested
		result.ValidBits = min(value.ValidBits, requested.Bits())
		if requested.Float() {
			result.ValidBits = requested.Bits()
		}
	}
	if result.Coding.Bytes() > 1 {
		result.Endian = sample.LittleEndian
	} else {
		result.Endian = sample.NoEndian
	}
	if _, ok := codecForCoding(result.Coding); !ok {
		result.Coding = sample.S16
		result.Endian = sample.LittleEndian
		result.ValidBits = min(value.ValidBits, result.Coding.Bits())
	}
	if !result.Valid() {
		return value
	}
	return result
}

func describedPackets(input stream.Descriptor, description sample.Description, target waveCodec) (stream.Descriptor, error) {
	properties, err := description.Apply(input.Properties())
	if err != nil {
		return stream.Descriptor{}, err
	}
	// The tag names the codec that writes this stream, which is not the one
	// that wrote the input when the caller asked for a different codec.
	properties = codec.WithoutBlock(codec.WithoutParameters(properties))
	if properties, err = codec.WithTag(properties, CodecTag(target.name)); err != nil {
		return stream.Descriptor{}, err
	}
	result, err := stream.NewDescriptor(input.ID(), codec.Packets().Descriptor(), timing.MustBase(1, int64(description.Rate)), properties)
	if err != nil {
		return stream.Descriptor{}, err
	}
	return result.WithMetadata(input.Metadata()), nil
}

// muxCodec decides which codec the output header declares.
//
// With no request the input codec is kept, which is what makes the default a
// copy. Otherwise the muxer names the codec it wants in the stream it asks
// for. Naming it is what reaches the component that writes it: a composition
// binds a tag to the coder for it, and a planner narrows candidates to that
// binding, so no other coder can answer to the name.
//
// What the muxer does not state is everything the coder decides -- the depth an
// expansion recovers, the geometry a coder chooses for its blocks. It does not
// have to: a gap closes when this Compile stops asking for anything, not when
// an input matches a descriptor named in advance.
func muxCodec(input stream.Descriptor, signal sample.Signal, requested string) (waveCodec, sample.Description, plugin.Requirement[stream.Descriptor], error) {
	var none plugin.Requirement[stream.Descriptor]
	description, linearErr := sample.FromProperties(input.Properties())
	target, named := codecNamed(requested)
	if requested != "" && !named {
		return waveCodec{}, sample.Description{}, none, unsupportedCodec(requested)
	}
	tag, tagged := codec.TagOf(input.Properties())
	current, known := codecOfTag(tag)

	if !named {
		if linearErr == nil {
			target, named = codecForCoding(description.Coding)
		} else {
			target, named = current, known
		}
		if !named {
			return waveCodec{}, sample.Description{}, none, unsupportedCodec(tag.String())
		}
	}
	if target.coding == "" {
		if tagged && current.name == target.name {
			return target, sample.Description{}, none, nil
		}
		desired, err := codedPackets(input, signal, target)
		if err != nil {
			return waveCodec{}, sample.Description{}, none, err
		}
		return waveCodec{}, sample.Description{}, plugin.Require("packets", plugin.DescriptorNeed("wave.codec", desired)), nil
	}
	// A stream stored one scalar each names its codec in its own description,
	// so the tag adds nothing a header needs. A coded one has only the tag.
	want := muxDescription(description, target.coding)
	if linearErr == nil && want == description {
		return target, description, none, nil
	}
	if linearErr != nil {
		want = sample.Description{
			Signal: signal, Coding: target.coding, Packing: sample.Interleaved, Endian: sample.LittleEndian,
		}
		if want.ValidBits == 0 {
			want.ValidBits = target.coding.Bits()
		}
		if target.coding.Bytes() == 1 {
			want.Endian = sample.NoEndian
		}
	}
	desired, err := describedPackets(input, want, target)
	if err != nil {
		return waveCodec{}, sample.Description{}, none, err
	}
	return waveCodec{}, sample.Description{}, plugin.Require("packets", plugin.DescriptorNeed("wave.sample-description", desired)), nil
}

// codedPackets is the stream a coder has to produce for this header: the same
// signal, carrying the tag that names the codec. What the coder states about
// its blocks is not named here, because only the coder knows it.
func codedPackets(input stream.Descriptor, signal sample.Signal, target waveCodec) (stream.Descriptor, error) {
	properties, err := sample.Signal{Rate: signal.Rate, Layout: signal.Layout}.Properties()
	if err != nil {
		return stream.Descriptor{}, err
	}
	if properties, err = codec.WithTag(properties, CodecTag(target.name)); err != nil {
		return stream.Descriptor{}, err
	}
	result, err := stream.NewDescriptor(input.ID(), codec.Packets().Descriptor(), timing.MustBase(1, int64(signal.Rate)), properties)
	if err != nil {
		return stream.Descriptor{}, err
	}
	return result.WithMetadata(input.Metadata()), nil
}

func unsupportedCodec(name string) error {
	return diagnostic.NewError(diagnostic.NewItem(
		"wave.codec", diagnostic.ErrorSeverity, diagnostic.Path{},
		"WAVE muxer cannot write a stream it has no format tag for: "+name, nil,
	))
}

// muxGeometry is the block geometry the output header states. A block-coded
// stream is reproduced rather than rebuilt, so it can only be written back
// under the codec it was read as.
func muxGeometry(outputCodec waveCodec, signal sample.Signal, input stream.Descriptor, inspected header) (blockGeometry, error) {
	if !outputCodec.blocked {
		return blockGeometry{}, nil
	}
	// A stream that was read as this codec is written back as it was read. One
	// that was coded for this output states its own geometry, because the
	// coder chose it.
	if inspected.codecTag == CodecTag(outputCodec.name) && inspected.geometry.stated() {
		return inspected.geometry, nil
	}
	block, blocked := codec.BlockOf(input.Properties())
	parameters, stated := codec.ParametersOf(input.Properties())
	if !blocked || !stated || block.Samples <= 0 {
		return blockGeometry{}, fmt.Errorf("%w: %s states no block geometry", ErrUnsupported, outputCodec.name)
	}
	rate := uint64(signal.Rate) * uint64(block.Bytes) / uint64(block.Samples)
	if rate > math.MaxUint32 {
		return blockGeometry{}, fmt.Errorf("%w: WAVE byte rate exceeds its header field", ErrUnsupported)
	}
	return blockGeometry{align: block.Bytes, byteRate: uint32(rate), parameters: parameters}, nil
}

// metadataLossEffects states in the Plan what the carrier could not say. The
// effect names the key rather than describing it, so a surface can group and
// count what a conversion would cost before it runs.
func metadataLossEffects(values []metadata.Loss) []plugin.Effect {
	if len(values) == 0 {
		return nil
	}
	result := make([]plugin.Effect, 0, len(values))
	for _, value := range values {
		result = append(result, plugin.Effect{
			Kind:   plugin.MetadataEffect,
			Loss:   plugin.Lossy,
			Detail: value.Detail,
			Item:   value.Key.String(),
		})
	}
	return result
}
