package mp4

import (
	"bytes"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/scratch"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

func TestMP4PluginIncludesExecutableRemuxer(t *testing.T) {
	var mux plugin.Component
	for _, component := range Plugin().Components() {
		if component.Identity() == MuxerIdentity() {
			mux = component
			break
		}
	}
	if mux.Identity().IsZero() || !mux.View().Executable || !mux.Ports().Equal(muxerShape()) {
		t.Fatal("MP4 plugin did not expose its executable remuxer")
	}
	trait, ok := mediaformat.WriteOf(mux)
	if !ok || trait.Format().Identity() != MP4().Identity() || !trait.Requirements().ValidFor(access.SinkDirection) {
		t.Fatal("MP4 muxer did not expose its RandomWrite format trait")
	}
}

func TestMP4MuxPreservesSourceRangesAndPatchesChunkOffsets(t *testing.T) {
	for _, afterMdat := range []bool{false, true} {
		t.Run(map[bool]string{false: "moov-before-mdat", true: "moov-after-mdat"}[afterMdat], func(t *testing.T) {
			data := twoTrackMovie(afterMdat, "isom", "iso2")
			if afterMdat {
				data = append(data, fixtureBox("free", []byte{0xaa, 0xbb, 0xcc})...)
			}
			inspected := inspectMovie(t, data)
			component, compiled := compileMP4Mux(t, inspected)
			if compiled.Scratch() != 16 || compiled.Resources().Memory != muxPageBytes {
				t.Fatalf("mux resources = scratch %d memory %d", compiled.Scratch(), compiled.Resources().Memory)
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
				for _, sample := range movieSamples(t, data, inspected, ordinal) {
					input := muxSample(t, data, sample, packets)
					if err := mux.Process(t.Context(), flow.NewSelectedBatch(ordinal, &input), collector); err != nil {
						t.Fatal(err)
					}
					if input.Valid() {
						t.Fatal("mux retained packet input")
					}
				}
			}
			if err := mux.Finalize(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := mux.Flush(t.Context(), collector); err != nil {
				t.Fatal(err)
			}
			encoded := applyMuxWrites(t, collector.items)
			if !bytes.Equal(encoded, data) {
				t.Fatalf("remux changed same-order source\n got %x\nwant %x", encoded, data)
			}
			if _, err := parseMovie(t.Context(), memoryRandom(encoded), uint64(len(encoded)), 1<<20, 1<<20); err != nil {
				t.Fatalf("remuxed movie did not parse: %v", err)
			}
			if buffers.Used() != 0 || packets.Used() != 0 {
				t.Fatalf("retained buffers = mux %d packets %d", buffers.Used(), packets.Used())
			}
		})
	}
}

func TestMP4MuxPreservesUnknownSampleEntryAsRawPackets(t *testing.T) {
	track := fixtureTrack{id: 9, timeScale: 90_000, handler: "vide", entryType: "zzzz", size: 2, sttsExtra: []fixtureTiming{{count: 1, duration: 3_000}}}
	data := fixtureMovie(false, "isom", []string{"iso2"}, []fixtureTrack{track}, [][]byte{fixtureBox("free", []byte{1, 2, 3})}, nil)
	inspected := inspectMovie(t, data)
	component, compiled := compileMP4Mux(t, inspected)
	buffers := mustMP4Allocator(t, 1<<20)
	packets := mustMP4Allocator(t, 2)
	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), buffers, journal)
	input := muxSample(t, data, movieSamples(t, data, inspected, 0)[0], packets)
	collector := &muxWriteCollector{}
	if err := mux.Process(t.Context(), flow.NewSelectedBatch(0, &input), collector); err != nil {
		t.Fatal(err)
	}
	if err := mux.Finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	if encoded := applyMuxWrites(t, collector.items); !bytes.Equal(encoded, data) {
		t.Fatal("unknown sample entry or opaque top-level box was not preserved")
	}
}

func TestMP4MuxPatchesOffsetsForTrackMajorOutput(t *testing.T) {
	want := twoTrackMovie(false, "isom", "iso2")
	data := reorderedTwoTrackMovie(t, want)
	inspected := inspectMovie(t, data)
	component, compiled := compileMP4Mux(t, inspected)
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
		for _, sample := range movieSamples(t, data, inspected, ordinal) {
			input := muxSample(t, data, sample, packets)
			if err := mux.Process(t.Context(), flow.NewSelectedBatch(ordinal, &input), collector); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := mux.Finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	if encoded := applyMuxWrites(t, collector.items); !bytes.Equal(encoded, want) {
		t.Fatalf("track-major output did not patch chunk offsets\n got %x\nwant %x", encoded, want)
	}
}

func TestMP4MuxZeroSampleTrackNeedsNoJournal(t *testing.T) {
	data := emptyTrackMovie()
	inspected := inspectMovie(t, data)
	component, compiled := compileMP4Mux(t, inspected)
	if compiled.Scratch() != 0 {
		t.Fatalf("empty movie scratch = %d", compiled.Scratch())
	}
	buffers := mustMP4Allocator(t, 1<<20)
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), buffers, nil)
	collector := &muxWriteCollector{}
	if err := mux.Finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	if encoded := applyMuxWrites(t, collector.items); !bytes.Equal(encoded, data) {
		t.Fatal("empty MP4 remux changed source")
	}
}
