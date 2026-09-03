package mp4

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/scratch"
	"github.com/godexture/godec/media/codec"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

func TestMP4MuxKeepsSelectedTrackOnly(t *testing.T) {
	for _, afterMdat := range []bool{false, true} {
		for _, selectedIndex := range []int{0, 1} {
			data := twoTrackMovie(afterMdat, "isom", "iso2")
			inspected := inspectMovie(t, data)
			selectedTrack := inspected.tracks[selectedIndex]
			properties, err := codec.WithTag(property.New(), SampleEntryTag(string(selectedTrack.codec[:])))
			if err != nil {
				t.Fatal(err)
			}
			input := stream.MustDescriptor(trackStreamID(selectedTrack.id), codec.Packets().Descriptor(), timing.MustBase(1, int64(selectedTrack.timeScale)), properties)
			context, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(MP4(), inspected))
			if err != nil {
				t.Fatal(err)
			}
			component := muxerComponent()
			resolved, err := component.Resolve(config.NewPatch())
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := plugin.Compile(component, context, resolved, flow.NewDescriptors(flow.Describe("packets", input)))
			if err != nil {
				t.Fatal(err)
			}
			if compiled.Scratch() != 8 {
				t.Fatalf("selected chunk journal = %d, want 8", compiled.Scratch())
			}
			buffers := mustMP4Allocator(t, 1<<20)
			packets := mustMP4Allocator(t, 8)
			journal, err := scratch.Open(compiled.Scratch())
			if err != nil {
				t.Fatal(err)
			}
			session := &recordingMovieSourceSession{movieSourceSession: &movieSourceSession{data: data, declaredSize: int64(len(data))}}
			mux := openMP4Mux(t, component, compiled, movieSourceOpeningForSession(t, session), buffers, journal)
			collector := &muxWriteCollector{}
			inputItem := muxSample(t, data, movieSamples(t, data, inspected, selectedIndex)[0], packets)
			if err := mux.Process(t.Context(), flow.NewSelectedBatch(0, &inputItem), collector); err != nil {
				t.Fatal(err)
			}
			if err := mux.finalize(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := mux.Flush(t.Context(), collector); err != nil {
				t.Fatal(err)
			}
			encoded := applyMuxWrites(t, collector.items)
			parsed := inspectMovie(t, encoded)
			if len(parsed.tracks) != 1 {
				t.Fatalf("subset result track count = %d", len(parsed.tracks))
			}
			if parsed.tracks[0].id != selectedTrack.id || parsed.media.payloadSize != selectedTrack.sampleBytes {
				t.Fatalf("subset result = track %d payload %d", parsed.tracks[0].id, parsed.media.payloadSize)
			}
			wantPayload := []byte{1, 2}
			if selectedIndex == 1 {
				wantPayload = []byte{2, 3, 4}
			}
			if !bytes.Equal(encoded[parsed.media.payloadOffset:parsed.media.payloadOffset+selectedTrack.sampleBytes], wantPayload) {
				t.Fatalf("subset payload = %x", encoded[parsed.media.payloadOffset:parsed.media.payloadOffset+selectedTrack.sampleBytes])
			}
			wantOffsetType := typeSTCO
			if selectedIndex == 1 {
				wantOffsetType = typeCO64
			}
			if parsed.tracks[0].tables.offsets.typeID != wantOffsetType {
				t.Fatalf("subset offset table = %q, want %q", parsed.tracks[0].tables.offsets.typeID, wantOffsetType)
			}
			unselectedIndex := 1 - selectedIndex
			unselectedSample := movieSamples(t, data, inspected, unselectedIndex)[0]
			for _, read := range session.reads {
				if readOverlaps(read, inspected.tracks[unselectedIndex].trak.offset, inspected.tracks[unselectedIndex].trak.size) {
					t.Fatalf("subset read unselected trak %d: %#v", inspected.tracks[unselectedIndex].id, read)
				}
				if readOverlaps(read, unselectedSample.offset, uint64(unselectedSample.size)) {
					t.Fatalf("subset read unselected sample %d: %#v", inspected.tracks[unselectedIndex].id, read)
				}
			}
			journal.Close()
		}
	}
}

func TestMP4MuxPatchesMovieDurationOnSubset(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	parsed := inspectMovie(t, data)
	binary.BigEndian.PutUint32(data[parsed.tracks[0].movieDuration.offset:], 100)
	binary.BigEndian.PutUint32(data[parsed.tracks[1].movieDuration.offset:], 700)
	parsed = inspectMovie(t, data)
	properties, err := codec.WithTag(property.New(), SampleEntryTag(string(parsed.tracks[1].codec[:])))
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor(trackStreamID(parsed.tracks[1].id), codec.Packets().Descriptor(), timing.MustBase(1, int64(parsed.tracks[1].timeScale)), properties)
	context, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(MP4(), parsed))
	if err != nil {
		t.Fatal(err)
	}
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, context, resolved, flow.NewDescriptors(flow.Describe("packets", input)))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), mustMP4Allocator(t, 1<<20), journal)
	packets := mustMP4Allocator(t, 8)
	collector := &muxWriteCollector{}
	item := muxSample(t, data, movieSamples(t, data, parsed, 1)[0], packets)
	if err := mux.Process(t.Context(), flow.NewSelectedBatch(0, &item), collector); err != nil {
		t.Fatal(err)
	}
	if err := mux.finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	result := inspectMovie(t, applyMuxWrites(t, collector.items))
	if result.header.duration.value != 700 {
		t.Fatalf("subset mvhd duration = %d", result.header.duration.value)
	}
}

func TestMP4MuxSubsetPreservesOpaqueAnchors(t *testing.T) {
	tracks := []fixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entryType: "mp4a", size: 2, directBefore: [][]byte{fixtureBox("free", []byte("REMOVED-BEFORE"))}, directAfter: [][]byte{fixtureBox("free", []byte("REMOVED-AFTER"))}, sttsExtra: []fixtureTiming{{count: 1, duration: 1024}}},
		{id: 2, timeScale: 1_000, handler: "vide", entryType: "avc1", size: 3, directBefore: [][]byte{fixtureBox("free", []byte("SELECTED-BEFORE"))}, directAfter: [][]byte{fixtureBox("free", []byte("SELECTED-AFTER"))}, sttsExtra: []fixtureTiming{{count: 1, duration: 40}}},
	}
	data := fixtureMovie(false, "isom", []string{"iso2"}, tracks, [][]byte{fixtureBox("free", []byte("TOP-MARKER"))}, [][]byte{fixtureBox("free", []byte("MOOV-MARKER"))})
	encoded := runSubsetMux(t, data, 1)
	for _, marker := range [][]byte{[]byte("TOP-MARKER"), []byte("MOOV-MARKER"), []byte("SELECTED-BEFORE"), []byte("SELECTED-AFTER")} {
		if !bytes.Contains(encoded, marker) {
			t.Fatalf("subset output lost marker %q", marker)
		}
	}
	for _, marker := range [][]byte{[]byte("REMOVED-BEFORE"), []byte("REMOVED-AFTER")} {
		if bytes.Contains(encoded, marker) {
			t.Fatalf("subset output retained removed marker %q", marker)
		}
	}
	ordered := [][]byte{[]byte("TOP-MARKER"), []byte("MOOV-MARKER"), []byte("SELECTED-BEFORE"), []byte("SELECTED-AFTER")}
	previous := -1
	for _, marker := range ordered {
		position := bytes.Index(encoded, marker)
		if position <= previous {
			t.Fatalf("opaque marker order position=%d previous=%d marker=%q", position, previous, marker)
		}
		previous = position
	}
}

// TestMP4MuxKeepsTrackReferencesAndEditsForAllTracks checks that the boxes a
// subset cannot preserve — track references and edit lists — do not block a full
// selection, which copies every trak verbatim.
func TestMP4MuxKeepsTrackReferencesAndEditsForAllTracks(t *testing.T) {
	tracks := []fixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entryType: "mp4a", size: 2, directBefore: [][]byte{fixtureBox("tref", fixtureBox("hint", fixtureU32(2)))}, sttsExtra: []fixtureTiming{{count: 1, duration: 1024}}},
		{id: 2, timeScale: 1_000, handler: "vide", entryType: "avc1", size: 3, directBefore: [][]byte{fixtureBox("edts", nil)}, sttsExtra: []fixtureTiming{{count: 1, duration: 40}}},
	}
	data := fixtureMovie(false, "isom", []string{"iso2"}, tracks, nil, nil)
	inspected := inspectMovie(t, data)
	component, compiled := compileMP4Mux(t, inspected)
	if compiled.Scratch() != 16 {
		t.Fatalf("all-track chunk journal = %d, want 16", compiled.Scratch())
	}
	buffers := mustMP4Allocator(t, 1<<20)
	packets := mustMP4Allocator(t, 8)
	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), buffers, journal)
	collector := &muxWriteCollector{}
	for ordinal := range inspected.tracks {
		item := muxSample(t, data, movieSamples(t, data, inspected, ordinal)[0], packets)
		if err := mux.Process(t.Context(), flow.NewSelectedBatch(ordinal, &item), collector); err != nil {
			t.Fatal(err)
		}
	}
	if err := mux.finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	if encoded := applyMuxWrites(t, collector.items); !bytes.Equal(encoded, data) {
		t.Fatal("all-track path changed a source with track references and edits")
	}
}

func runSubsetMux(t testing.TB, data []byte, selectedIndex int) []byte {
	t.Helper()
	inspected := inspectMovie(t, data)
	selected := inspected.tracks[selectedIndex]
	properties, err := codec.WithTag(property.New(), SampleEntryTag(string(selected.codec[:])))
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor(trackStreamID(selected.id), codec.Packets().Descriptor(), timing.MustBase(1, int64(selected.timeScale)), properties)
	if inspected.metadata.Scope().Valid() {
		input = input.WithMetadata(inspected.metadata)
	}
	context, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(MP4(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, context, resolved, flow.NewDescriptors(flow.Describe("packets", input)))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), mustMP4Allocator(t, 1<<20), journal)
	packets := mustMP4Allocator(t, 8)
	item := muxSample(t, data, movieSamples(t, data, inspected, selectedIndex)[0], packets)
	collector := &muxWriteCollector{}
	if err := mux.Process(t.Context(), flow.NewSelectedBatch(0, &item), collector); err != nil {
		t.Fatal(err)
	}
	if err := mux.finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	return applyMuxWrites(t, collector.items)
}

func openSubsetMuxForTest(t testing.TB, data []byte, source access.Opening, journal plugin.Scratch) (*muxer, movie) {
	t.Helper()
	inspected := inspectMovie(t, data)
	selected := inspected.tracks[1]
	properties, err := codec.WithTag(property.New(), SampleEntryTag(string(selected.codec[:])))
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor(trackStreamID(selected.id), codec.Packets().Descriptor(), timing.MustBase(1, int64(selected.timeScale)), properties)
	if inspected.metadata.Scope().Valid() {
		input = input.WithMetadata(inspected.metadata)
	}
	context, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(MP4(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plugin.Compile(component, context, resolved, flow.NewDescriptors(flow.Describe("packets", input)))
	if err != nil {
		t.Fatal(err)
	}
	return openMP4Mux(t, component, compiled, source, mustMP4Allocator(t, 1<<20), journal), inspected
}
