package mp4

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/media/buffer"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/packet"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

var componentTestDomain flow.Collector

type movieSourceSession struct {
	data         []byte
	declaredSize int64
}

func (*movieSourceSession) Close() error { return nil }

func (*movieSourceSession) Capabilities() access.Capabilities {
	value, _ := access.NewCapabilities(access.RandomRead, access.StableSize)
	return value
}

func (s *movieSourceSession) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	if offset < 0 || offset >= int64(len(s.data)) {
		return 0, io.EOF
	}
	count := copy(destination, s.data[offset:])
	if count != len(destination) {
		return count, io.EOF
	}
	return count, nil
}

func (s *movieSourceSession) Size(context.Context) (int64, error) {
	if s.declaredSize != 0 {
		return s.declaredSize, nil
	}
	return int64(len(s.data)), nil
}

func movieSourceOpening(t testing.TB, data []byte) access.Opening {
	return movieSourceOpeningWithSize(t, data, int64(len(data)))
}

func movieSourceOpeningWithSize(t testing.TB, data []byte, size int64) access.Opening {
	t.Helper()
	session := &movieSourceSession{data: data, declaredSize: size}
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

func inspectMovie(t testing.TB, data []byte) movie {
	t.Helper()
	inspection, err := inspectMP4(mediaformat.NewInspectContext(t.Context(), movieSourceOpening(t, data), plugin.CompileContext{}, 1<<20, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	value, ok := inspectionValue[movie](inspection)
	if !ok {
		t.Fatal("MP4 inspection did not carry movie")
	}
	return value
}

func inspectionValue[T any](value mediaformat.Inspection) (T, bool) {
	ctx, err := mediaformat.WithInspection(plugin.CompileContext{}, value)
	if err != nil {
		var zero T
		return zero, false
	}
	return mediaformat.InspectionOf[T](ctx, MP4())
}

func compileMP4(t testing.TB, inspected movie) (plugin.Component, plugin.Compilation) {
	t.Helper()
	context, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(MP4(), inspected))
	if err != nil {
		t.Fatal(err)
	}
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

type packetCollector struct {
	items []*flow.Item[packet.Packet]
}

func (*packetCollector) Own(into *flow.Item[packet.Packet], value packet.Packet) {
	into.Bind(codec.Packets(), &componentTestDomain)
	into.Set(value)
}

func (c *packetCollector) Emit(_ context.Context, input *flow.Item[packet.Packet]) error {
	if !input.Valid() {
		return errors.New("collector received an unowned packet")
	}
	stored := new(flow.Item[packet.Packet])
	stored.Bind(codec.Packets(), &componentTestDomain)
	stored.Move(input)
	c.items = append(c.items, stored)
	return nil
}

type packetRoutes struct{ routes []packetCollector }

func (r *packetRoutes) Route(ordinal int) (flow.Emitter[packet.Packet], bool) {
	if ordinal < 0 || ordinal >= len(r.routes) {
		return nil, false
	}
	return &r.routes[ordinal], true
}

func TestSetIsExactlyTheOwnedPluginComposition(t *testing.T) {
	actual, err := host.New(host.Plugins(Set()))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := host.New(host.Plugins(plugin.NewSet(Plugin())))
	if err != nil {
		t.Fatal(err)
	}
	if actual.Catalog().Fingerprint() != expected.Catalog().Fingerprint() {
		t.Fatal("mp4.Plugin does not own its complete composition")
	}
}

func TestMP4ProbeNeedsMatchesAndRejects(t *testing.T) {
	needs, err := probeMP4(mediaformat.NewProbeContext(t.Context(), nil))
	if err != nil || needs.Status() != mediaformat.ProbeNeedsData || len(needs.Needs()) != 1 || needs.Needs()[0].Offset() != 0 || needs.Needs()[0].Length() != 8 {
		t.Fatalf("initial probe = %#v, %v", needs, err)
	}
	nonFTYP, err := probeMP4(mediaformat.NewProbeContext(t.Context(), []access.ProbeView{access.NewProbeView([]byte("not an M"))}))
	if err != nil || nonFTYP.Status() != mediaformat.ProbeMismatch {
		t.Fatalf("8-byte non-ftyp probe = %#v, %v", nonFTYP, err)
	}
	ftypHeader := fixtureFileType("isom", "iso2")[:8]
	needPrefix, err := probeMP4(mediaformat.NewProbeContext(t.Context(), []access.ProbeView{access.NewProbeView(ftypHeader)}))
	if err != nil || needPrefix.Status() != mediaformat.ProbeNeedsData || len(needPrefix.Needs()) != 1 || needPrefix.Needs()[0].Offset() != 0 || needPrefix.Needs()[0].Length() != 16 {
		t.Fatalf("8-byte ftyp probe = %#v, %v", needPrefix, err)
	}
	short, err := probeMP4(mediaformat.NewProbeContextAtEnd(t.Context(), []access.ProbeView{access.NewProbeView([]byte{0, 0, 0, 20})}, 4))
	if err != nil || short.Status() != mediaformat.ProbeMismatch {
		t.Fatalf("short known-end probe = %#v, %v", short, err)
	}
	mismatch, err := probeMP4(mediaformat.NewProbeContextAtEnd(t.Context(), []access.ProbeView{access.NewProbeView([]byte("not an MP4 file"))}, 15))
	if err != nil || mismatch.Status() != mediaformat.ProbeMismatch {
		t.Fatalf("mismatch probe = %#v, %v", mismatch, err)
	}
	ftyp := fixtureFileType("isom", "iso2")
	match, err := probeMP4(mediaformat.NewProbeContextAtEnd(t.Context(), []access.ProbeView{access.NewProbeView(ftyp)}, int64(len(ftyp))))
	if err != nil || match.Status() != mediaformat.ProbeMatch {
		t.Fatalf("match probe = %#v, %v", match, err)
	}
	badSize := append([]byte(nil), ftyp...)
	badSize[3] = 18
	malformed, err := probeMP4(mediaformat.NewProbeContextAtEnd(t.Context(), []access.ProbeView{access.NewProbeView(badSize)}, int64(len(badSize))))
	if err != nil || malformed.Status() != mediaformat.ProbeMismatch {
		t.Fatalf("non-4cc brand-list probe = %#v, %v", malformed, err)
	}
	if SampleEntryTag("avc1").String() != "mp4:avc1" || SampleEntryTag("bad").Valid() {
		t.Fatal("SampleEntryTag did not validate a sample-entry fourcc")
	}
}

func TestInspectMP4UsesLimitsAndRejectsMultipleDescriptions(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	_, err := inspectMP4(mediaformat.NewInspectContext(t.Context(), movieSourceOpening(t, data), plugin.CompileContext{}, 8, 1<<20))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("limited inspection error = %v", err)
	}
	multiple := fixtureTrack{id: 1, timeScale: 1000, handler: "vide", entryType: "avc1", descriptions: 2, size: 1, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}}
	data = fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{multiple}, nil, nil)
	_, err = inspectMP4(mediaformat.NewInspectContext(t.Context(), movieSourceOpening(t, data), plugin.CompileContext{}, 1<<20, 1<<20))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("multi-description inspection error = %v", err)
	}
}

func TestMP4CompileDescribesTracksInInspectionOrder(t *testing.T) {
	inspected := inspectMovie(t, twoTrackMovie(false, "isom", "iso2"))
	_, compiled := compileMP4(t, inspected)
	outputs, ok := plugin.OutputsOf[stream.Descriptor](compiled)
	if !ok || outputs.Len() != 2 {
		t.Fatalf("MP4 outputs = %#v/%v", outputs, ok)
	}
	for index, want := range []struct {
		id   stream.ID
		base timing.Base
		tag  string
	}{{"1", timing.MustBase(1, 48000), "mp4:mp4a"}, {"2", timing.MustBase(1, 1000), "mp4:avc1"}} {
		value := outputs.At("packets")[index]
		if value.ID() != want.id || value.Schema() != codec.Packets().Identity() || value.TimeBase() != want.base {
			t.Fatalf("output %d = %#v", index, value)
		}
		tag, ok := codec.TagOf(value.Properties())
		if !ok || tag.String() != want.tag {
			t.Fatalf("output %d tag = %q/%v", index, tag, ok)
		}
	}
	if compiled.Resources().Memory != 3 {
		t.Fatalf("MP4 payload memory = %d, want max sample size 3", compiled.Resources().Memory)
	}
}

func TestMP4CompileRequiresInspection(t *testing.T) {
	component := demuxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	_, err = plugin.Compile(component, plugin.CompileContext{}, resolved, flow.NewDescriptors[stream.Descriptor]())
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "plugin.compile" {
		t.Fatalf("Compile without inspection error = %v", err)
	}
}

func TestMP4DemuxesTracksFromTheBorrowedSource(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	component, compiled := compileMP4(t, inspectMovie(t, data))
	// The direct collector retains both output packets for assertion. A real
	// downstream consumes each routed packet before the next one is emitted;
	// Compile still requests only the largest one-packet payload below.
	allocator, err := buffer.NewAllocator(8)
	if err != nil {
		t.Fatal(err)
	}
	operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpening(t, data)}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	routes := &packetRoutes{routes: make([]packetCollector, 2)}
	reader, ok := operator.(flow.RoutedReader[packet.Packet])
	if !ok {
		t.Fatal("MP4 operator is not a routed packet reader")
	}
	for range 2 {
		if err := reader.Read(t.Context(), routes); err != nil {
			t.Fatal(err)
		}
	}
	if err := reader.Read(t.Context(), routes); !errors.Is(err, io.EOF) {
		t.Fatalf("MP4 reader completion = %v, want EOF after emitting call", err)
	}
	if len(routes.routes[0].items) != 1 || len(routes.routes[1].items) != 1 {
		t.Fatalf("routes = %d/%d", len(routes.routes[0].items), len(routes.routes[1].items))
	}
	first, second := routes.routes[0].items[0].Value(), routes.routes[1].items[0].Value()
	if first.Sequence() != 0 || first.PTS().Value() != 0 || first.DTS().Value() != 0 || first.Duration().Value() != 1024 || string(first.Bytes().AppendTo(nil)) != "\x01\x02" {
		t.Fatalf("first packet = %#v %v", first, first.Bytes().AppendTo(nil))
	}
	if second.Sequence() != 0 || second.PTS().Value() != -2 || second.DTS().Value() != 0 || second.Duration().Value() != 40 || string(second.Bytes().AppendTo(nil)) != "\x02\x03\x04" {
		t.Fatalf("second packet = %#v %v", second, second.Bytes().AppendTo(nil))
	}
	for _, route := range routes.routes {
		for _, item := range route.items {
			item.Drop()
		}
	}
	if allocator.Used() != 0 {
		t.Fatalf("retained output buffers = %d", allocator.Used())
	}
}

func TestMP4RoutedReaderEmitsEverySampleBeforeEOF(t *testing.T) {
	data, samples := mixedTableMovie(false)
	component, compiled := compileMP4(t, inspectMovie(t, data))
	allocator, _ := buffer.NewAllocator(14)
	operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpening(t, data)}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	routes := &packetRoutes{routes: make([]packetCollector, 1)}
	reader := operator.(flow.RoutedReader[packet.Packet])
	for index := range samples {
		if err := reader.Read(t.Context(), routes); err != nil {
			t.Fatalf("MP4 routed sample %d Read = %v", index, err)
		}
		if got := len(routes.routes[0].items); got != index+1 {
			t.Fatalf("MP4 routed samples after Read %d = %d, want %d", index, got, index+1)
		}
	}
	if got := len(routes.routes[0].items); got != len(samples) {
		t.Fatalf("MP4 routed samples = %d, want %d", got, len(samples))
	}
	for index, item := range routes.routes[0].items {
		value := item.Value()
		want := samples[index]
		if value.Sequence() != want.sequence-1 || value.PTS().Value() != timing.NewPTS(want.pts) || value.DTS().Value() != timing.NewDTS(int64(want.dts)) || value.Duration().Value() != timing.NewDuration(int64(want.duration)) || value.Bytes().Len() != int(want.size) {
			t.Fatalf("MP4 routed sample %d = %#v, want %#v", index, value, want)
		}
		start := int(want.offset)
		end := start + int(want.size)
		if got := value.Bytes().AppendTo(nil); !bytes.Equal(got, data[start:end]) {
			t.Fatalf("MP4 routed sample %d payload = %v, want %v", index, got, data[start:end])
		}
		item.Drop()
	}
	if err := reader.Read(t.Context(), routes); !errors.Is(err, io.EOF) {
		t.Fatalf("MP4 routed reader completion = %v, want EOF", err)
	}
	if allocator.Used() != 0 {
		t.Fatalf("MP4 routed reader retained %d payload bytes", allocator.Used())
	}
}

func TestMP4RoutedReaderReturnsImmediateEOFWithoutSamples(t *testing.T) {
	data := emptyTrackMovie()
	component, compiled := compileMP4(t, inspectMovie(t, data))
	allocator, _ := buffer.NewAllocator(1)
	operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpening(t, data)}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	routes := &packetRoutes{routes: make([]packetCollector, 1)}
	if err := operator.(flow.RoutedReader[packet.Packet]).Read(t.Context(), routes); !errors.Is(err, io.EOF) {
		t.Fatalf("empty MP4 routed reader = %v, want EOF", err)
	}
	if len(routes.routes[0].items) != 0 || allocator.Used() != 0 {
		t.Fatalf("empty MP4 routed reader emitted %d items and retained %d bytes", len(routes.routes[0].items), allocator.Used())
	}
}

func TestMP4OpenRejectsMissingAndChangedSource(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	component, compiled := compileMP4(t, inspectMovie(t, data))
	allocator, _ := buffer.NewAllocator(3)
	_, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator}), compiled)
	if err == nil {
		t.Fatal("MP4 Open accepted a missing inspected source")
	}
	_, err = component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpening(t, data[:len(data)-1])}), compiled)
	if err == nil {
		t.Fatal("MP4 Open accepted a short source")
	}
}

func TestMP4DemuxKeepsUnknownSampleEntriesAsRawPackets(t *testing.T) {
	track := fixtureTrack{id: 9, timeScale: 90_000, handler: "vide", entryType: "zzzz", size: 2, sttsExtra: []fixtureTiming{{count: 1, duration: 3_000}}}
	data := fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{track}, nil, nil)
	component, compiled := compileMP4(t, inspectMovie(t, data))
	outputs, _ := plugin.OutputsOf[stream.Descriptor](compiled)
	tag, ok := codec.TagOf(outputs.At("packets")[0].Properties())
	if !ok || tag != SampleEntryTag("zzzz") {
		t.Fatalf("unknown sample-entry tag = %q/%v", tag, ok)
	}
	allocator, _ := buffer.NewAllocator(2)
	operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpening(t, data)}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	routes := &packetRoutes{routes: make([]packetCollector, 1)}
	if err := operator.(flow.RoutedReader[packet.Packet]).Read(t.Context(), routes); err != nil {
		t.Fatal(err)
	}
	if got := routes.routes[0].items[0].Value().Bytes().AppendTo(nil); string(got) != "\x09\x0a" {
		t.Fatalf("unknown-codec packet payload = %v", got)
	}
	routes.routes[0].items[0].Drop()
}

func TestMP4DemuxFailsClosedForCanceledTruncatedAndMissingRoutes(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	component, compiled := compileMP4(t, inspectMovie(t, data))
	allocator, _ := buffer.NewAllocator(8)

	t.Run("canceled", func(t *testing.T) {
		operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpening(t, data)}), compiled)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err = operator.(flow.RoutedReader[packet.Packet]).Read(ctx, &packetRoutes{routes: make([]packetCollector, 2)})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Read = %v", err)
		}
	})

	t.Run("truncated payload", func(t *testing.T) {
		one := fixtureTrack{id: 1, timeScale: 1_000, handler: "vide", entryType: "avc1", size: 3, sttsExtra: []fixtureTiming{{count: 1, duration: 1}}}
		complete := fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{one}, nil, nil)
		localComponent, localCompiled := compileMP4(t, inspectMovie(t, complete))
		operator, err := localComponent.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpeningWithSize(t, complete[:len(complete)-1], int64(len(complete)))}), localCompiled)
		if err != nil {
			t.Fatal(err)
		}
		err = operator.(flow.RoutedReader[packet.Packet]).Read(t.Context(), &packetRoutes{routes: make([]packetCollector, 1)})
		if !errors.Is(err, ErrTruncated) {
			t.Fatalf("truncated Read = %v", err)
		}
	})

	t.Run("missing route", func(t *testing.T) {
		operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpening(t, data)}), compiled)
		if err != nil {
			t.Fatal(err)
		}
		routes := &packetRoutes{routes: make([]packetCollector, 1)}
		reader := operator.(flow.RoutedReader[packet.Packet])
		if err := reader.Read(t.Context(), routes); err != nil || len(routes.routes[0].items) != 1 {
			t.Fatalf("first routed Read = %v, items = %d", err, len(routes.routes[0].items))
		}
		err = reader.Read(t.Context(), routes)
		if !errors.Is(err, ErrMalformed) {
			t.Fatalf("missing later route Read = %v", err)
		}
		if sticky := reader.Read(t.Context(), routes); sticky != err {
			t.Fatalf("sticky missing route Read = %v, want original %v", sticky, err)
		}
		for _, item := range routes.routes[0].items {
			item.Drop()
		}
	})
}

func TestMP4DemuxCloseDropsBorrowedSourceViews(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	component, compiled := compileMP4(t, inspectMovie(t, data))
	allocator, _ := buffer.NewAllocator(3)
	operator, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: allocator, Source: movieSourceOpening(t, data)}), compiled)
	if err != nil {
		t.Fatal(err)
	}
	demux, ok := operator.(*demuxer)
	if !ok {
		t.Fatal("MP4 Open did not return its demuxer")
	}
	if err := demux.Close(); err != nil {
		t.Fatal(err)
	}
	if demux.reader != nil || demux.buffers != nil {
		t.Fatal("MP4 Close retained borrowed source or payload allocator")
	}
}

var _ flow.RoutedReader[packet.Packet] = (*demuxer)(nil)
