package mp4

import (
	"fmt"
	"strconv"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type demuxPlan struct {
	shape flow.Shape
	movie movie
}

func demuxerShape() flow.Shape {
	return flow.NewShape(
		[]flow.Port{flow.In("bytes", access.Bytes())},
		[]flow.Port{flow.Out("packets", codec.Packets(), flow.Many())},
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
					Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("mp4.input"))},
				}, nil
			}
			inspected, ok := mediaformat.InspectionOf[movie](ctx, MP4())
			if !ok || !inspected.valid() {
				return plugin.Compiled[demuxPlan, stream.Descriptor]{}, diagnostic.NewError(diagnostic.NewItem(
					"mp4.inspection", diagnostic.ErrorSeverity, diagnostic.Path{}, "MP4 demuxer requires a prepared movie inspection", nil,
				))
			}
			return compileDemux(shape, input, inspected)
		},
		Open: func(ctx plugin.OpenContext, plan demuxPlan) (flow.Operator, error) {
			return openDemuxer(ctx, plan)
		},
	}
	return plugin.NewComponent[demuxerID](plugin.Descriptor{DisplayName: "MP4 demuxer"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithRouter("bytes", access.Bytes(), "packets", codec.Packets()),
		mediaformat.Read(MP4(), access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)), mediaformat.WithProbe(probeMP4), mediaformat.WithInspect(inspectMP4)),
	)
}

func compileDemux(shape flow.Shape, input stream.Descriptor, inspected movie) (plugin.Compiled[demuxPlan, stream.Descriptor], error) {
	if err := validateDemuxMovie(inspected); err != nil {
		return plugin.Compiled[demuxPlan, stream.Descriptor]{}, err
	}
	outputs := make([]flow.PortDescriptor[stream.Descriptor], 0, len(inspected.tracks))
	memory := resource.Bytes(1)
	for _, value := range inspected.tracks {
		properties, err := codec.WithTag(property.New(), SampleEntryTag(string(value.codec[:])))
		if err != nil {
			return plugin.Compiled[demuxPlan, stream.Descriptor]{}, err
		}
		descriptor, err := stream.NewDescriptor(trackStreamID(value.id), codec.Packets().Descriptor(), timing.MustBase(1, int64(value.timeScale)), properties)
		if err != nil {
			return plugin.Compiled[demuxPlan, stream.Descriptor]{}, err
		}
		outputs = append(outputs, flow.Describe("packets", descriptor))
		if size := resource.Bytes(value.maxSampleSize); size > memory {
			memory = size
		}
	}
	return plugin.Compiled[demuxPlan, stream.Descriptor]{
		Plan:      demuxPlan{shape: shape.Clone(), movie: inspected},
		Outputs:   flow.NewDescriptors(outputs...),
		Effects:   []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "mp4-demux"}},
		Resources: resource.Request{Memory: memory},
	}, nil
}

func validateDemuxMovie(value movie) error {
	if !value.valid() {
		return fmt.Errorf("%w: MP4 inspection is invalid", ErrMalformed)
	}
	for _, track := range value.tracks {
		if track.descriptionCount != 1 {
			return fmt.Errorf("%w: track %d has %d sample descriptions", ErrUnsupported, track.id, track.descriptionCount)
		}
	}
	return nil
}

func trackStreamID(value uint32) stream.ID {
	return stream.ID(strconv.FormatUint(uint64(value), 10))
}
