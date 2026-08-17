package wave

import (
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type demuxPlan struct {
	shape  flow.Shape
	header header
}

func demuxerShape() flow.Shape {
	return flow.NewShape(
		[]flow.Port{flow.In("bytes", access.Bytes())},
		[]flow.Port{flow.Out("chunks", mediaformat.Chunks())},
	)
}

func demuxerComponent() plugin.Component {
	shape := demuxerShape()
	spec := plugin.Spec[configuration, demuxPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(ctx plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[demuxPlan, stream.Descriptor], error) {
			input, ok := inputs.One("bytes")
			if !ok {
				return plugin.Compiled[demuxPlan, stream.Descriptor]{
					Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("wave.input"))},
				}, nil
			}
			inspected, ok := mediaformat.InspectionOf[header](ctx, WAVE())
			if !ok || !inspected.valid() {
				return plugin.Compiled[demuxPlan, stream.Descriptor]{}, diagnostic.NewError(diagnostic.NewItem(
					"wave.inspection", diagnostic.ErrorSeverity, diagnostic.Path{}, "WAVE demuxer requires a prepared header inspection", nil,
				))
			}
			properties, err := inspected.description.Properties()
			if err != nil {
				return plugin.Compiled[demuxPlan, stream.Descriptor]{}, err
			}
			properties, err = codec.WithTag(properties, inspected.codecTag)
			if err != nil {
				return plugin.Compiled[demuxPlan, stream.Descriptor]{}, err
			}
			output, err := stream.NewDescriptor(input.ID(), mediaformat.Chunks().Descriptor(), timing.MustBase(1, int64(inspected.description.Rate)), properties)
			if err != nil {
				return plugin.Compiled[demuxPlan, stream.Descriptor]{}, err
			}
			document, err := metadata.NewBuilder(metadata.StreamScope).
				Append(input.Metadata()).
				Append(inspected.metadata).
				Build()
			if err != nil {
				return plugin.Compiled[demuxPlan, stream.Descriptor]{}, err
			}
			output = output.WithMetadata(document)
			return plugin.Compiled[demuxPlan, stream.Descriptor]{
				Plan:      demuxPlan{shape: shape.Clone(), header: inspected},
				Outputs:   flow.NewDescriptors(flow.Describe("chunks", output)),
				Effects:   []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "wave-demux"}},
				Resources: resource.Request{Memory: resource.Bytes(inspected.blockAlign)},
			}, nil
		},
		Open: func(ctx plugin.OpenContext, plan demuxPlan) (flow.Operator, error) {
			if !plan.header.valid() {
				return nil, errors.New("WAVE demux plan is invalid")
			}
			if ctx.Buffers() == nil {
				return nil, errors.New("WAVE demuxer requires a payload buffer grant")
			}
			return newDemuxer(plan, ctx.Buffers()), nil
		},
	}
	return plugin.NewComponent[demuxerID](plugin.Descriptor{DisplayName: "WAVE demuxer"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithProcessor("bytes", access.Bytes(), "chunks", mediaformat.Chunks()),
		mediaformat.Read(WAVE(), access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)), mediaformat.WithProbe(probeWAVE), mediaformat.WithInspect(inspectWAVE)),
	)
}
