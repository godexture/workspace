package mp4

import (
	"fmt"
	"math"
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
	shape     flow.Shape
	sourceEnd uint64
	tracks    []demuxTrack
}

type demuxTrack struct {
	inspectionIndex int
	value           track
}

func demuxerShape() flow.Shape {
	return flow.NewShape(
		nil,
		[]flow.Port{flow.Out("packets", codec.Packets(), flow.Many())},
	)
}

func demuxerComponent() plugin.Component {
	shape := demuxerShape()
	spec := plugin.Spec[configuration, demuxPlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(ctx plugin.CompileContext, _ configuration, _ flow.Descriptors[stream.Descriptor]) (plugin.Compiled[demuxPlan, stream.Descriptor], error) {
			inspected, ok := mediaformat.InspectionOf[movie](ctx, MP4())
			if !ok || !inspected.valid() {
				return plugin.Compiled[demuxPlan, stream.Descriptor]{}, diagnostic.NewError(diagnostic.NewItem(
					"mp4.inspection", diagnostic.ErrorSeverity, diagnostic.Path{}, "MP4 demuxer requires a prepared movie inspection", nil,
				))
			}
			selection, selected := mediaformat.SelectionOf(ctx, MP4())
			return compileDemux(shape, inspected, selection, selected)
		},
		Open: func(ctx plugin.OpenContext, plan demuxPlan) (flow.Operator, error) {
			return openDemuxer(ctx, plan)
		},
	}
	return plugin.NewComponent[demuxerID](plugin.Descriptor{DisplayName: "MP4 demuxer"}, configurationSchema(),
		plugin.WithSpec(spec),
		plugin.WithRoutedReader("packets", codec.Packets()),
		mediaformat.Read(MP4(), access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)), mediaformat.WithProbe(probeMP4), mediaformat.WithInspect(inspectMP4)),
	)
}

func compileDemux(shape flow.Shape, inspected movie, selection mediaformat.Selection, selected bool) (plugin.Compiled[demuxPlan, stream.Descriptor], error) {
	if err := validateInspectedMovie(inspected); err != nil {
		return plugin.Compiled[demuxPlan, stream.Descriptor]{}, err
	}
	tracks, err := selectDemuxTracks(inspected, selection, selected)
	if err != nil {
		return plugin.Compiled[demuxPlan, stream.Descriptor]{}, err
	}
	for _, selectedTrack := range tracks {
		if err := validateDemuxTrack(selectedTrack.value); err != nil {
			return plugin.Compiled[demuxPlan, stream.Descriptor]{}, err
		}
	}
	outputs := make([]flow.PortDescriptor[stream.Descriptor], 0, len(tracks))
	memory := resource.Bytes(1)
	for _, selectedTrack := range tracks {
		value := selectedTrack.value
		properties, err := trackProperties(value)
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
		Plan:      demuxPlan{shape: shape.Clone(), sourceEnd: inspected.sourceEnd, tracks: tracks},
		Outputs:   flow.NewDescriptors(outputs...),
		Effects:   []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "mp4-demux"}},
		Resources: resource.Request{Memory: memory},
	}, nil
}

func selectDemuxTracks(inspected movie, selection mediaformat.Selection, selected bool) ([]demuxTrack, error) {
	if !selected {
		result := make([]demuxTrack, len(inspected.tracks))
		for index, value := range inspected.tracks {
			result[index] = demuxTrack{inspectionIndex: index, value: value}
		}
		return result, nil
	}
	if !selection.Valid() || selection.Format().Identity() != MP4().Identity() {
		return nil, fmt.Errorf("%w: MP4 stream selection is invalid", ErrUnsupported)
	}
	ids := selection.Streams()
	wanted := make(map[stream.ID]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	result := make([]demuxTrack, 0, len(wanted))
	for index, value := range inspected.tracks {
		id := trackStreamID(value.id)
		if _, ok := wanted[id]; !ok {
			continue
		}
		result = append(result, demuxTrack{inspectionIndex: index, value: value})
		delete(wanted, id)
	}
	if len(wanted) != 0 {
		for _, id := range ids {
			if _, missing := wanted[id]; missing {
				return nil, fmt.Errorf("%w: selected stream %q is absent from MP4 inspection", ErrUnsupported, id)
			}
		}
		return nil, fmt.Errorf("%w: MP4 stream selection cannot be represented", ErrUnsupported)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%w: MP4 stream selection is empty", ErrUnsupported)
	}
	return result, nil
}

func validateInspectedMovie(value movie) error {
	if !value.valid() {
		return fmt.Errorf("%w: MP4 inspection is invalid", ErrMalformed)
	}
	return nil
}

func validateDemuxTrack(value track) error {
	if !value.valid() {
		return fmt.Errorf("%w: MP4 track model is invalid", ErrMalformed)
	}
	if value.descriptionCount != 1 {
		return fmt.Errorf("%w: track %d has %d sample descriptions", ErrUnsupported, value.id, value.descriptionCount)
	}
	return nil
}

func trackStreamID(value uint32) stream.ID {
	return stream.ID(strconv.FormatUint(uint64(value), 10))
}

// trackProperties carries the sample-entry tag a codec binding keys on, plus the
// audio description of a linear PCM track. The description is published only
// when the media timescale is the sample rate, so the packet time base a decoder
// receives agrees with the description it reads.
//
// An edited track is never described: its edts maps media time onto the
// presentation timeline, and a decoder that only sees samples would silently
// produce the unedited media. Copying the track keeps the edts, so the track
// still passes through -- it just cannot be decoded.
func trackProperties(value track) (property.Set, error) {
	properties := property.New()
	if value.audio.Valid() && !value.edits && uint64(value.audio.Rate) == uint64(value.timeScale) {
		var err error
		if properties, err = value.audio.Properties(); err != nil {
			return property.Set{}, err
		}
	}
	// A track records how long it lasts in the timescale its samples are
	// counted in, which is the one this descriptor states, so a consumer that
	// needs the end knows it before the first sample is read. The all-ones
	// value ISO BMFF writes when the length is unknown states nothing.
	if !value.movieDuration.unknown() && value.duration <= math.MaxInt64 {
		var err error
		if properties, err = stream.WithDuration(properties, timing.NewDuration(int64(value.duration))); err != nil {
			return property.Set{}, err
		}
	}
	return codec.WithTag(properties, SampleEntryTag(string(value.codec[:])))
}
