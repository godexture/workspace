package mp4

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/scratch"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
)

func TestMP4MuxSubsetFailuresAreSticky(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	t.Run("canceled", func(t *testing.T) {
		journal, err := scratch.Open(8)
		if err != nil {
			t.Fatal(err)
		}
		defer journal.Close()
		mux, inspected := openSubsetMuxForTest(t, data, movieSourceOpening(t, data), journal)
		packets := mustMP4Allocator(t, 8)
		item := muxSample(t, data, movieSamples(t, data, inspected, 1)[0], packets)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := mux.Process(ctx, flow.NewSelectedBatch(0, &item), &muxWriteCollector{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("subset canceled Process = %v", err)
		}
		second := muxSample(t, data, movieSamples(t, data, inspected, 1)[0], packets)
		if err := mux.Process(t.Context(), flow.NewSelectedBatch(0, &second), &muxWriteCollector{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("subset canceled sticky Process = %v", err)
		}
	})
	t.Run("truncated", func(t *testing.T) {
		full := append(twoTrackMovie(false, "isom", "iso2"), fixtureBox("free", []byte{1, 2, 3})...)
		journal, err := scratch.Open(8)
		if err != nil {
			t.Fatal(err)
		}
		defer journal.Close()
		mux, inspected := openSubsetMuxForTest(t, full, movieSourceOpeningWithSize(t, full[:len(full)-1], int64(len(full))), journal)
		packets := mustMP4Allocator(t, 8)
		item := muxSample(t, full, movieSamples(t, full, inspected, 1)[0], packets)
		collector := &muxWriteCollector{}
		if err := mux.Process(t.Context(), flow.NewSelectedBatch(0, &item), collector); err != nil {
			t.Fatal(err)
		}
		if err := mux.Finalize(t.Context()); err != nil {
			t.Fatal(err)
		}
		flushErr := mux.Flush(t.Context(), collector)
		if !errors.Is(flushErr, ErrTruncated) || mux.Flush(t.Context(), collector) != flushErr {
			t.Fatalf("subset truncated Flush = %v", flushErr)
		}
		for _, output := range collector.items {
			output.Drop()
		}
	})
	t.Run("scratch quota", func(t *testing.T) {
		journal := &muxMemoryScratch{appendErr: errors.New("scratch quota")}
		mux, inspected := openSubsetMuxForTest(t, data, movieSourceOpening(t, data), journal)
		packets := mustMP4Allocator(t, 8)
		item := muxSample(t, data, movieSamples(t, data, inspected, 1)[0], packets)
		collector := &muxWriteCollector{}
		if err := mux.Process(t.Context(), flow.NewSelectedBatch(0, &item), collector); err != nil {
			t.Fatal(err)
		}
		finalizeErr := mux.Finalize(t.Context())
		if finalizeErr == nil || mux.Finalize(t.Context()) != finalizeErr {
			t.Fatalf("subset scratch sticky Finalize = %v", finalizeErr)
		}
		for _, output := range collector.items {
			output.Drop()
		}
	})
}

func muxLayoutFixtureTracks() []fixtureTrack {
	return []fixtureTrack{
		{id: 1, timeScale: 48_000, handler: "soun", entryType: "mp4a", size: 2, sttsExtra: []fixtureTiming{{count: 1, duration: 1024}}},
		{id: 2, timeScale: 1_000, handler: "vide", entryType: "avc1", size: 3, sttsExtra: []fixtureTiming{{count: 1, duration: 40}}},
	}
}

func muxInputDescriptor(t testing.TB, value track) stream.Descriptor {
	t.Helper()
	properties, err := codec.WithTag(property.New(), SampleEntryTag(string(value.codec[:])))
	if err != nil {
		t.Fatal(err)
	}
	return stream.MustDescriptor(trackStreamID(value.id), codec.Packets().Descriptor(), timing.MustBase(1, int64(value.timeScale)), properties)
}

func compileMuxSelection(t testing.TB, inspected movie, indexes ...int) (muxLayout, error) {
	t.Helper()
	inputs := make([]stream.Descriptor, 0, len(indexes))
	for _, index := range indexes {
		inputs = append(inputs, muxInputDescriptor(t, inspected.tracks[index]))
	}
	return compileMux(inputs, inspected)
}

func TestMP4MuxCompileRejectsUnsafeStructure(t *testing.T) {
	compile := func(t testing.TB, data []byte, indexes ...int) error {
		t.Helper()
		_, err := compileMuxSelection(t, inspectMovie(t, data), indexes...)
		return err
	}
	t.Run("dropped tref target", func(t *testing.T) {
		tracks := muxLayoutFixtureTracks()
		tracks[0].directBefore = [][]byte{fixtureBox("tref", fixtureBox("hint", fixtureU32(2)))}
		if err := compile(t, fixtureMovie(false, "isom", []string{"iso2"}, tracks, nil, nil), 0); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("dropped tref error = %v", err)
		}
	})
	// A rebuilt mdat invalidates any byte offset recorded outside the sample
	// tables, whether or not the selection drops a track.
	for _, unsafe := range []struct {
		name     string
		topLevel [][]byte
		inMoov   [][]byte
	}{
		{name: "sidx", topLevel: [][]byte{fixtureBox("sidx", nil)}},
		{name: "meta iloc", topLevel: [][]byte{fixtureBox("meta", append(fixtureFullBox(0, 0, nil), fixtureBox("iloc", nil)...))}},
		{name: "moov meta iloc", inMoov: [][]byte{fixtureBox("meta", append(fixtureFullBox(0, 0, nil), fixtureBox("iloc", nil)...))}},
	} {
		t.Run(unsafe.name, func(t *testing.T) {
			data := fixtureMovie(false, "isom", []string{"iso2"}, muxLayoutFixtureTracks(), unsafe.topLevel, unsafe.inMoov)
			if err := compile(t, data, 1); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("subset error = %v", err)
			}
			if err := compile(t, data, 0, 1); !errors.Is(err, ErrUnsupported) {
				t.Fatalf("all-track error = %v", err)
			}
		})
	}
	t.Run("input order", func(t *testing.T) {
		data := fixtureMovie(false, "isom", []string{"iso2"}, muxLayoutFixtureTracks(), nil, nil)
		if _, err := compileMuxSelection(t, inspectMovie(t, data), 1, 0); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("input reorder error = %v", err)
		}
	})
	t.Run("unknown input", func(t *testing.T) {
		data := fixtureMovie(false, "isom", []string{"iso2"}, muxLayoutFixtureTracks(), nil, nil)
		inspected := inspectMovie(t, data)
		value := muxInputDescriptor(t, inspected.tracks[0])
		unknown := stream.MustDescriptor("99", value.SchemaDescriptor(), value.TimeBase(), value.Properties())
		if _, err := compileMux([]stream.Descriptor{unknown}, inspected); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("unknown input error = %v", err)
		}
	})
	t.Run("duplicate input", func(t *testing.T) {
		data := fixtureMovie(false, "isom", []string{"iso2"}, muxLayoutFixtureTracks(), nil, nil)
		if _, err := compileMuxSelection(t, inspectMovie(t, data), 0, 0); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("duplicate input error = %v", err)
		}
	})
	t.Run("source offset overflow", func(t *testing.T) {
		data := fixtureMovie(false, "isom", []string{"iso2"}, muxLayoutFixtureTracks(), nil, nil)
		inspected := inspectMovie(t, data)
		inspected.sourceEnd = math.MaxUint64
		if _, err := compileMuxSelection(t, inspected, 1); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("source overflow error = %v", err)
		}
	})
	t.Run("track layout overlap", func(t *testing.T) {
		data := fixtureMovie(false, "isom", []string{"iso2"}, muxLayoutFixtureTracks(), nil, nil)
		inspected := inspectMovie(t, data)
		inspected.tracks[1].trak.offset = inspected.tracks[0].trak.offset
		if _, err := compileMuxSelection(t, inspected, 1); !errors.Is(err, ErrMalformed) {
			t.Fatalf("track layout error = %v", err)
		}
	})
}

// TestMP4MuxLayoutKeepsFullSelectionByteExact checks the shared layout: a full
// selection reuses the inspected moov and mdat headers and copies every box,
// while a partial selection resizes both and rebuilds the payload.
func TestMP4MuxLayoutKeepsFullSelectionByteExact(t *testing.T) {
	data := fixtureMovie(false, "isom", []string{"iso2"}, muxLayoutFixtureTracks(), nil, nil)
	inspected := inspectMovie(t, data)
	// Give the dropped track the longest duration, so only a partial selection
	// has to shorten the movie.
	binary.BigEndian.PutUint32(data[inspected.tracks[0].movieDuration.offset:], 700)
	binary.BigEndian.PutUint32(data[inspected.tracks[1].movieDuration.offset:], 300)
	binary.BigEndian.PutUint32(data[inspected.header.duration.offset:], 700)
	inspected = inspectMovie(t, data)

	full, err := compileMuxSelection(t, inspected, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if full.size != inspected.sourceEnd || full.duration.width != 0 {
		t.Fatalf("full layout size = %d of %d, duration patch = %#v", full.size, inspected.sourceEnd, full.duration)
	}
	if full.payloadOffset() != inspected.media.payloadOffset || full.payloadSize() != inspected.media.payloadSize {
		t.Fatalf("full layout payload = %d+%d", full.payloadOffset(), full.payloadSize())
	}
	for _, piece := range full.pieces {
		if piece.kind == muxHeader {
			t.Fatalf("full layout synthesized a header: %#v", piece)
		}
	}
	partial, err := compileMuxSelection(t, inspected, 1)
	if err != nil {
		t.Fatal(err)
	}
	dropped := inspected.tracks[0]
	if partial.size != inspected.sourceEnd-dropped.trak.size-dropped.sampleBytes {
		t.Fatalf("partial layout size = %d", partial.size)
	}
	if partial.payloadSize() != inspected.tracks[1].sampleBytes || len(partial.tracks) != 1 || partial.tracks[0].source != 1 {
		t.Fatalf("partial layout = %#v", partial.tracks)
	}
	if partial.duration.width != 4 || partial.duration.value != 300 {
		t.Fatalf("partial duration patch = %#v", partial.duration)
	}
}
