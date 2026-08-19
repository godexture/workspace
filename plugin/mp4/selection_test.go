package mp4

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type recordedMovieRead struct {
	offset int64
	length int
}

type recordingMovieSourceSession struct {
	*movieSourceSession
	reads []recordedMovieRead
}

func (s *recordingMovieSourceSession) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	s.reads = append(s.reads, recordedMovieRead{offset: offset, length: len(destination)})
	return s.movieSourceSession.ReadAt(ctx, destination, offset)
}

func TestMP4SelectionKeepsInspectionOrderAndSelectedResourceClaim(t *testing.T) {
	data := fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{
		{id: 3, timeScale: 48_000, handler: "soun", entryType: "mp4a", size: 2, sttsExtra: []fixtureTiming{{count: 1, duration: 1024}}},
		{id: 20, timeScale: 1_000, handler: "vide", entryType: "avc1", size: 3, sttsExtra: []fixtureTiming{{count: 1, duration: 40}}},
	}, nil, nil)
	inspected := inspectMovie(t, data)
	_, compiled := compileSelectedMP4(t, inspected, "3", "20")
	outputs, ok := plugin.OutputsOf[stream.Descriptor](compiled)
	if !ok || outputs.Len() != 2 {
		t.Fatalf("selected outputs = %#v/%v", outputs, ok)
	}
	values := outputs.At("packets")
	if values[0].ID() != "3" || values[1].ID() != "20" {
		t.Fatalf("selected output order = %q, %q", values[0].ID(), values[1].ID())
	}

	_, firstOnly := compileSelectedMP4(t, inspected, "3")
	firstOutputs, _ := plugin.OutputsOf[stream.Descriptor](firstOnly)
	if firstOutputs.Len() != 1 || firstOutputs.At("packets")[0].ID() != "3" || firstOnly.Resources().Memory != 2 {
		t.Fatalf("first-track compilation = outputs %#v, memory %d", firstOutputs, firstOnly.Resources().Memory)
	}
}

func TestMP4SelectionRejectsMissingAndEmptyStreamSets(t *testing.T) {
	inspected := inspectMovie(t, twoTrackMovie(false, "isom", "iso2"))
	ctx := selectedMP4CompileContext(t, inspected, "missing")
	component := demuxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	_, err = plugin.Compile(component, ctx, resolved, flow.NewDescriptors[stream.Descriptor]())
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "plugin.compile" || !strings.Contains(items[0].Detail["cause"], "selected stream \"missing\" is absent") {
		t.Fatalf("missing selection error = %v, diagnostics %#v", err, items)
	}
	if _, err := mediaformat.NewSelection(MP4()); !errors.Is(err, mediaformat.ErrInvalidSelection) {
		t.Fatalf("empty MP4 selection error = %v", err)
	}
}

func TestMP4SelectionValidatesOnlySelectedTrackCapabilities(t *testing.T) {
	data := fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entryType: "mp4a", descriptions: 2, size: 2, sttsExtra: []fixtureTiming{{count: 1, duration: 1024}}},
		{id: 2, timeScale: 1_000, handler: "vide", entryType: "avc1", size: 3, sttsExtra: []fixtureTiming{{count: 1, duration: 40}}},
	}, nil, nil)
	inspected := inspectMovie(t, data)
	_, compiled := compileSelectedMP4(t, inspected, "2")
	outputs, _ := plugin.OutputsOf[stream.Descriptor](compiled)
	if outputs.Len() != 1 || outputs.At("packets")[0].ID() != "2" {
		t.Fatalf("normal selected track outputs = %#v", outputs)
	}
	unsupported, err := mediaformat.NewSelection(MP4(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileDemux(demuxerShape(), inspected, unsupported, true); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("selected multi-description track error = %v", err)
	}
}

func TestMP4SelectionUsesCompactRouteAndSkipsUnselectedTrackIO(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	inspected := inspectMovie(t, data)
	component, compiled := compileSelectedMP4(t, inspected, "2")
	allocator, err := buffer.NewAllocator(3)
	if err != nil {
		t.Fatal(err)
	}
	session := &recordingMovieSourceSession{movieSourceSession: &movieSourceSession{data: data, declaredSize: int64(len(data))}}
	operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpeningForSession(t, session)}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	demux := operator.(*demuxer)
	if len(demux.tracks) != 1 || demux.tracks[0].inspectionIndex != 1 || demux.tracks[0].value.id != 2 || len(demux.items) != 1 {
		t.Fatalf("selected demux plan = tracks %#v items %d", demux.tracks, len(demux.items))
	}
	routes := &packetRoutes{routes: make([]packetCollector, 1)}
	reader := operator.(flow.RoutedReader[packet.Packet])
	if err := reader.Read(t.Context(), routes); err != nil {
		t.Fatal(err)
	}
	if len(routes.routes[0].items) != 1 || string(routes.routes[0].items[0].Value().Bytes().AppendTo(nil)) != "\x02\x03\x04" {
		t.Fatalf("compact route 0 packets = %#v", routes.routes[0].items)
	}
	if err := reader.Read(t.Context(), routes); !errors.Is(err, io.EOF) {
		t.Fatalf("selected reader completion = %v", err)
	}
	if err := reader.Read(t.Context(), routes); !errors.Is(err, io.EOF) {
		t.Fatalf("selected reader sticky completion = %v", err)
	}

	firstCursor, err := newSampleCursor(t.Context(), memoryRandom(data), inspected.tracks[0])
	if err != nil {
		t.Fatal(err)
	}
	firstSample, more, err := firstCursor.next(t.Context())
	if err != nil || !more {
		t.Fatalf("first unselected sample = %#v, %v, %v", firstSample, more, err)
	}
	for _, read := range session.reads {
		if readOverlaps(read, inspected.tracks[0].trak.offset, inspected.tracks[0].trak.size) {
			t.Fatalf("selected demux read unselected track table range: %#v", read)
		}
		if readOverlaps(read, firstSample.offset, uint64(firstSample.size)) {
			t.Fatalf("selected demux read unselected track payload: %#v", read)
		}
	}
	routes.routes[0].items[0].Drop()
	if allocator.Used() != 0 {
		t.Fatalf("selected demux retained %d payload bytes", allocator.Used())
	}
}

func TestMP4SelectedDemuxFailuresRemainSticky(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	component, compiled := compileSelectedMP4(t, inspectMovie(t, data), "2")
	allocator, _ := buffer.NewAllocator(3)

	t.Run("canceled", func(t *testing.T) {
		operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpening(t, data)}), compiled)
		if err != nil {
			t.Fatal(err)
		}
		reader := operator.(flow.RoutedReader[packet.Packet])
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		failure := reader.Read(ctx, &packetRoutes{routes: make([]packetCollector, 1)})
		if !errors.Is(failure, context.Canceled) {
			t.Fatalf("selected canceled Read = %v", failure)
		}
		if sticky := reader.Read(t.Context(), &packetRoutes{routes: make([]packetCollector, 1)}); sticky != failure {
			t.Fatalf("selected canceled sticky Read = %v, want %v", sticky, failure)
		}
	})

	t.Run("missing compact route", func(t *testing.T) {
		operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpening(t, data)}), compiled)
		if err != nil {
			t.Fatal(err)
		}
		reader := operator.(flow.RoutedReader[packet.Packet])
		failure := reader.Read(t.Context(), &packetRoutes{})
		if !errors.Is(failure, ErrMalformed) {
			t.Fatalf("selected missing route Read = %v", failure)
		}
		if sticky := reader.Read(t.Context(), &packetRoutes{routes: make([]packetCollector, 1)}); sticky != failure {
			t.Fatalf("selected missing route sticky Read = %v, want %v", sticky, failure)
		}
	})
}

func compileSelectedMP4(t testing.TB, inspected movie, ids ...stream.ID) (plugin.Component, plugin.Compilation) {
	t.Helper()
	context := selectedMP4CompileContext(t, inspected, ids...)
	component := demuxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, context, resolved, flow.NewDescriptors[stream.Descriptor]())
	if err != nil {
		t.Fatal(err)
	}
	return component, compiled
}

func selectedMP4CompileContext(t testing.TB, inspected movie, ids ...stream.ID) plugin.CompileContext {
	t.Helper()
	ctx, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(MP4(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	selection, err := mediaformat.NewSelection(MP4(), ids...)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = mediaformat.WithSelection(ctx, selection)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func movieSourceOpeningForSession(t testing.TB, session access.Session) access.Opening {
	t.Helper()
	selection, ok := access.Select(session.Capabilities(), access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)))
	if !ok {
		t.Fatal("MP4 test source did not satisfy random stable requirements")
	}
	opening, err := access.NewOpening(access.SourceDirection, session, selection, 0)
	if err != nil {
		t.Fatal(err)
	}
	return opening
}

func readOverlaps(value recordedMovieRead, offset, size uint64) bool {
	if value.offset < 0 || size == 0 || value.length == 0 {
		return false
	}
	start := uint64(value.offset)
	end := start + uint64(value.length)
	return start < offset+size && offset < end
}
