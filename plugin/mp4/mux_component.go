package mp4

import (
	"fmt"
	"math"

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

const muxPageBytes = 64 * 1024
const muxJournalPageBytes = 8 * 1024

type muxPlan struct {
	shape   flow.Shape
	movie   movie
	scratch resource.Bytes
}

func muxerShape() flow.Shape {
	return flow.NewShape(
		[]flow.Port{flow.In("packets", codec.Packets(), flow.Many(), flow.WithFanIn(flow.SerialFanIn))},
		[]flow.Port{flow.Out("writes", access.Writes())},
	)
}

func muxerComponent() plugin.Component {
	shape := muxerShape()
	spec := plugin.Spec[configuration, muxPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(ctx plugin.CompileContext, _ configuration, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[muxPlan, stream.Descriptor], error) {
			packets := inputs.At("packets")
			if len(packets) == 0 {
				return plugin.Compiled[muxPlan, stream.Descriptor]{
					Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("packets", plugin.ConditionNeed[stream.Descriptor]("mp4.input"))},
				}, nil
			}
			inspected, ok := mediaformat.InspectionOf[movie](ctx, MP4())
			if !ok || !inspected.valid() {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, diagnostic.NewError(diagnostic.NewItem(
					"mp4.inspection", diagnostic.ErrorSeverity, diagnostic.Path{}, "MP4 muxer requires a prepared movie inspection", nil,
				))
			}
			scratch, err := compileMux(shape, packets, inspected)
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
			}
			output, err := stream.NewDescriptor("mp4", access.Writes().Descriptor(), timing.Base{}, property.New())
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[muxPlan, stream.Descriptor]{
				Plan:         muxPlan{shape: shape.Clone(), movie: inspected, scratch: scratch},
				Outputs:      flow.NewDescriptors(flow.Describe("writes", output)),
				Effects:      []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "mp4-remux"}},
				Resources:    resource.Request{Memory: muxPageBytes},
				Scratch:      scratch,
				Finalization: plugin.RequiresFinalization,
			}, nil
		},
		Open: func(ctx plugin.OpenContext, plan muxPlan) (flow.Operator, error) {
			return openMuxer(ctx, plan)
		},
		Finalizes: true,
	}
	return plugin.NewComponent[muxerID](plugin.Descriptor{DisplayName: "MP4 muxer"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithJoiner("packets", codec.Packets(), flow.SerialFanIn, "writes", access.Writes()),
		mediaformat.Write(MP4(), access.NewRequirements(access.AllOf(access.RandomWrite))),
	)
}

func compileMux(shape flow.Shape, inputs []stream.Descriptor, inspected movie) (resource.Bytes, error) {
	if err := validateMuxMovie(inspected); err != nil {
		return 0, err
	}
	if len(inputs) != len(inspected.tracks) {
		return 0, fmt.Errorf("%w: MP4 mux requires %d packet inputs in inspection order, got %d", ErrUnsupported, len(inspected.tracks), len(inputs))
	}
	for index, input := range inputs {
		track := inspected.tracks[index]
		if !input.Valid() || !input.SchemaDescriptor().Equal(codec.Packets().Descriptor()) || input.ID() != trackStreamID(track.id) || input.TimeBase() != timing.MustBase(1, int64(track.timeScale)) {
			return 0, fmt.Errorf("%w: packet input %d does not match inspected track %d", ErrUnsupported, index, track.id)
		}
		tag, ok := codec.TagOf(input.Properties())
		if !ok || tag != SampleEntryTag(string(track.codec[:])) {
			return 0, fmt.Errorf("%w: packet input %d changes track %d sample entry", ErrUnsupported, index, track.id)
		}
		if input.Metadata().Len() != 0 || len(input.Metadata().Blocks()) != 0 {
			return 0, fmt.Errorf("%w: packet input %d changes MP4 metadata", ErrUnsupported, index)
		}
	}
	return muxScratchBytes(inspected)
}

func validateMuxMovie(value movie) error {
	if err := validateInspectedMovie(value); err != nil {
		return err
	}
	for _, track := range value.tracks {
		if err := validateDemuxTrack(track); err != nil {
			return err
		}
	}
	if value.totalSampleBytes != value.media.payloadSize {
		return fmt.Errorf("%w: mdat payload %d does not exactly match %d referenced sample bytes", ErrUnsupported, value.media.payloadSize, value.totalSampleBytes)
	}
	return nil
}

func muxScratchBytes(value movie) (resource.Bytes, error) {
	var total uint64
	for _, track := range value.tracks {
		chunks := uint64(track.chunkCount)
		if chunks > (uint64(math.MaxInt64)-total)/8 {
			return 0, fmt.Errorf("%w: MP4 chunk offset journal exceeds runtime range", ErrUnsupported)
		}
		total += chunks * 8
	}
	return resource.Bytes(total), nil
}
