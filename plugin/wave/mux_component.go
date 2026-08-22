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
	spec := plugin.Spec[configuration, muxPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(ctx plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[muxPlan, stream.Descriptor], error) {
			input, ok := inputs.One("packets")
			if !ok {
				return plugin.Compiled[muxPlan, stream.Descriptor]{
					Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("packets", plugin.ConditionNeed[stream.Descriptor]("wave.input"))},
				}, nil
			}
			description, err := sample.FromProperties(input.Properties())
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, diagnostic.NewError(diagnostic.NewItem(
					"wave.sample-description", diagnostic.ErrorSeverity, diagnostic.Path{}, "WAVE muxer requires a complete PCM sample description", nil,
				))
			}
			// WAVE stores interleaved little-endian samples. Ask the planner to
			// convert anything else rather than refusing the stream outright.
			if want := muxDescription(description); want != description {
				desired, err := describedPackets(input, want)
				if err != nil {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
				}
				return plugin.Compiled[muxPlan, stream.Descriptor]{
					Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("packets", plugin.DescriptorNeed("wave.sample-description", desired))},
				}, nil
			}
			var muxHeaderValue muxHeader
			var rewrite metadata.Document
			var rewriteNeeded bool
			inspected, handedOff := mediaformat.InspectionOf[header](ctx, WAVE())
			if handedOff {
				if !inspected.valid() {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, fmt.Errorf("%w: WAVE mux received an invalid inspection", ErrMalformed)
				}
				if inspected.ranges.any() && description != inspected.description {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, fmt.Errorf("%w: WAVE source ranges cannot be reused after changing the PCM description", ErrUnsupported)
				}
				if len(input.Metadata().Blocks()) != 0 {
					return plugin.Compiled[muxPlan, stream.Descriptor]{}, fmt.Errorf("%w: WAVE mux metadata contains source blobs without a range inspection handoff", ErrUnsupported)
				}
				muxHeaderValue, err = newRangeMuxHeader(description, inspected)
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
				muxHeaderValue, err = newMuxHeaderWithChunks(description, chunks)
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
	return plugin.NewComponent[muxerID](plugin.Descriptor{DisplayName: "WAVE muxer"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("packets", codec.Packets(), "writes", access.Writes()),
		mediaformat.Write(WAVE(), access.NewRequirements(access.AllOf(access.RandomWrite))),
	)
}

// muxDescription is the closest description this muxer can write for the one it
// received. An unrepresentable rate or channel layout is left unchanged so the
// header marshaller reports it.
func muxDescription(value sample.Description) sample.Description {
	result := value
	result.Format = sample.S16Interleaved
	result.Endian = sample.LittleEndian
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
