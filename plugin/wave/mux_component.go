package wave

import (
	"errors"
	"fmt"

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
			outputCodec, description, requirement, err := muxCodec(input, signal, configuration.Coding)
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
				muxHeaderValue, err = newRangeMuxHeader(outputCodec, signal, !inspected.ranges.any() && outputCodec.coding == "", inspected)
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
				muxHeaderValue, err = newMuxHeaderWithChunks(outputCodec, signal, outputCodec.coding == "", chunks)
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
				Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "wave-mux"}},
				Resources: resource.Request{Memory: resource.Bytes(func() int {
					if muxHeaderValue.rangeMode {
						return max(muxHeaderValue.payloadBytes(), wavePageSize)
					}
					return muxHeaderValue.payloadBytes()
				}())},
				Finalization: plugin.RequiresFinalization,
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
		Finalizes: true,
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

func describedPackets(input stream.Descriptor, description sample.Description) (stream.Descriptor, error) {
	properties, err := description.Apply(input.Properties())
	if err != nil {
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
// A stream stored one scalar each can be written under any linear codec WAVE
// names, so a representation this muxer cannot write becomes a requirement the
// planner satisfies with a converter. A companded stream is written back under
// the codec tag it arrived with: its samples are opaque here, so the only
// faithful output is the one that reproduces them.
func muxCodec(input stream.Descriptor, signal sample.Signal, requested sample.Coding) (waveCodec, sample.Description, plugin.Requirement[stream.Descriptor], error) {
	var none plugin.Requirement[stream.Descriptor]
	description, linearErr := sample.FromProperties(input.Properties())
	if linearErr == nil {
		want := muxDescription(description, requested)
		if want != description {
			desired, err := describedPackets(input, want)
			if err != nil {
				return waveCodec{}, sample.Description{}, none, err
			}
			return waveCodec{}, sample.Description{}, plugin.Require("packets", plugin.DescriptorNeed("wave.sample-description", desired)), nil
		}
		entry, ok := codecForCoding(description.Coding)
		if !ok {
			return waveCodec{}, sample.Description{}, none, unsupportedCodec(string(description.Coding))
		}
		return entry, description, none, nil
	}
	tag, tagged := codec.TagOf(input.Properties())
	entry, known := codecOfTag(tag)
	if !tagged || !known {
		return waveCodec{}, sample.Description{}, none, unsupportedCodec(tag.String())
	}
	if requested.Valid() {
		// Rewriting companded samples into a linear coding means decoding
		// them, and the depth that decoding recovers is the codec's to state,
		// not this header's. Until a caller needs it, say so instead of
		// guessing a description the planner would have to match exactly.
		return waveCodec{}, sample.Description{}, none, fmt.Errorf("%w: cannot rewrite %s into %s", ErrUnsupported, entry.name, requested)
	}
	return entry, sample.Description{}, none, nil
}

func unsupportedCodec(name string) error {
	return diagnostic.NewError(diagnostic.NewItem(
		"wave.codec", diagnostic.ErrorSeverity, diagnostic.Path{},
		"WAVE muxer cannot write a stream it has no format tag for: "+name, nil,
	))
}
