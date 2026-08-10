package wave

import (
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type muxPlan struct {
	shape  flow.Shape
	header muxHeader
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
		Shape: plugin.StaticShape[configuration](shape),
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
			resolver, _ := metadata.ResolverOf(ctx)
			chunks, err := marshalMuxChunks(ctx.Context(), resolver, input.Metadata())
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
			}
			header, err := newMuxHeaderWithChunks(description, chunks)
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
			}
			output, err := stream.NewDescriptor(input.ID(), access.Writes().Identity(), access.CarrierTimeBase(), property.New())
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[muxPlan, stream.Descriptor]{
				Plan:         muxPlan{shape: shape.Clone(), header: header},
				Outputs:      flow.NewDescriptors(flow.Describe("writes", output.WithMetadata(input.Metadata()))),
				Effects:      []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "wave-mux"}},
				Resources:    resource.Request{Memory: resource.Bytes(header.payloadBytes())},
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
			return newMuxer(plan, ctx.Buffers()), nil
		},
		Finalizes: true,
	}
	return plugin.NewComponent[muxerID](plugin.Descriptor{DisplayName: "WAVE muxer"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("packets", codec.Packets(), "writes", access.Writes()),
		mediaformat.Write(WAVE(), access.AnyOf(access.RandomWrite)),
	)
}
