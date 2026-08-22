package mp4

import (
	"fmt"

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

// muxJournalTrackPageBytes is held once per selected track, so it stays well
// under the page the patch phase reads back through the buffer grant.
const muxJournalTrackPageBytes = 1024

type muxPlan struct {
	shape   flow.Shape
	movie   movie
	layout  muxLayout
	scratch resource.Bytes
}

func muxerShape() flow.Shape {
	return flow.NewShape(
		// The mdat payload is laid out in the order the packets arrive, so this
		// port needs its producer's own emit order rather than whatever order
		// separate tasks would deliver in.
		[]flow.Port{flow.In("packets", codec.Packets(), flow.Many(), flow.WithFanIn(flow.SerialFanIn), flow.Direct())},
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
			layout, err := compileMux(packets, inspected)
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
			}
			scratch := layout.journalBytes()
			output, err := stream.NewDescriptor("mp4", access.Writes().Descriptor(), timing.Base{}, property.New())
			if err != nil {
				return plugin.Compiled[muxPlan, stream.Descriptor]{}, err
			}
			return plugin.Compiled[muxPlan, stream.Descriptor]{
				Plan:         muxPlan{shape: shape.Clone(), movie: inspected, layout: layout, scratch: scratch},
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

// validateMuxLayout re-checks at Open that the compiled layout still describes
// the inspection it was planned against.
func validateMuxLayout(value movie, layout muxLayout) error {
	if err := validateMuxMovie(value); err != nil {
		return err
	}
	if !layout.valid() || layout.size == 0 {
		return fmt.Errorf("%w: MP4 output layout is incomplete", ErrMalformed)
	}
	previous := -1
	var payload uint64
	for _, selected := range layout.tracks {
		if selected.source <= previous || selected.source >= len(value.tracks) {
			return fmt.Errorf("%w: MP4 selected track order is invalid", ErrMalformed)
		}
		if selected.value.id != value.tracks[selected.source].id || selected.value.trak != value.tracks[selected.source].trak {
			return fmt.Errorf("%w: MP4 selected track %d no longer matches the inspection", ErrMalformed, selected.value.id)
		}
		var ok bool
		if payload, ok = checkedBoxAdd(payload, selected.value.sampleBytes); !ok {
			return fmt.Errorf("%w: MP4 selected sample bytes overflow", ErrMalformed)
		}
		previous = selected.source
	}
	if payload != layout.payloadSize() {
		return fmt.Errorf("%w: MP4 output payload does not cover the selected samples", ErrMalformed)
	}
	return nil
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
