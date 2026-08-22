package mp4

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"

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

func TestMP4MuxRejectsUncoveredMdatDuringCompile(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	parsed := inspectMovie(t, data)
	data = append(data, 0xee)
	binary.BigEndian.PutUint32(data[parsed.media.offset:parsed.media.offset+4], uint32(parsed.media.size+1))
	inspected := inspectMovie(t, data)
	if inspected.totalSampleBytes == inspected.media.payloadSize {
		t.Fatal("test movie did not contain mdat padding")
	}
	compileContext, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(MP4(), inspected))
	if err != nil {
		t.Fatal(err)
	}
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	properties, _ := codec.WithTag(property.New(), SampleEntryTag("mp4a"))
	input := stream.MustDescriptor("1", codec.Packets().Descriptor(), timing.MustBase(1, 48_000), properties)
	_, err = plugin.Compile(component, compileContext, resolved, flow.NewDescriptors(flow.Describe("packets", input), flow.Describe("packets", input)))
	if err == nil {
		t.Fatalf("padding compile error = %v", err)
	}
}

func TestMP4MuxCompileAndOpenRequireFrozenInspectionAndSource(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	inspected := inspectMovie(t, data)
	component := muxerComponent()
	resolved, err := component.Resolve(config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	properties, err := codec.WithTag(property.New(), SampleEntryTag("mp4a"))
	if err != nil {
		t.Fatal(err)
	}
	input := stream.MustDescriptor("1", codec.Packets().Descriptor(), timing.MustBase(1, 48_000), properties)
	if _, err := plugin.Compile(component, plugin.CompileContext{}, resolved, flow.NewDescriptors(flow.Describe("packets", input))); err == nil {
		t.Fatal("MP4 mux Compile accepted no inspection")
	}

	component, compiled := compileMP4Mux(t, inspected)
	buffers := mustMP4Allocator(t, muxPageBytes)
	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if _, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: buffers, Scratch: journal}), compiled); err == nil {
		t.Fatal("MP4 mux Open accepted no source")
	}
	if _, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{Buffers: buffers, Source: movieSourceOpening(t, data)}), compiled); err == nil {
		t.Fatal("MP4 mux Open accepted no chunk-offset journal")
	}
	if _, err := component.Open(plugin.NewOpenContext(t.Context(), plugin.OpenServices{
		Buffers: buffers,
		Scratch: journal,
		Source:  movieSourceOpening(t, data[:len(data)-1]),
	}), compiled); err == nil {
		t.Fatal("MP4 mux Open accepted a changed source size")
	}
}

func TestMP4MuxRejectsOutOfOrderPacketsAndDropsOwnership(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	inspected := inspectMovie(t, data)
	component, compiled := compileMP4Mux(t, inspected)
	buffers := mustMP4Allocator(t, 1<<20)
	packets := mustMP4Allocator(t, 3)
	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), buffers, journal)
	input := muxSample(t, data, movieSamples(t, data, inspected, 1)[0], packets)
	err = mux.Process(t.Context(), flow.NewSelectedBatch(1, &input), &muxWriteCollector{})
	if !errors.Is(err, ErrUnsupported) || input.Valid() || packets.Used() != 0 {
		t.Fatalf("out-of-order Process = %v input=%t used=%d", err, input.Valid(), packets.Used())
	}
}

func TestMP4MuxRejectsPacketMutationAndTrackReturn(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	inspected := inspectMovie(t, data)
	component, compiled := compileMP4Mux(t, inspected)
	buffers := mustMP4Allocator(t, 1<<20)
	packets := mustMP4Allocator(t, 8)
	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()

	t.Run("timing mutation", func(t *testing.T) {
		mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), buffers, journal)
		sample := movieSamples(t, data, inspected, 0)[0]
		sample.duration++
		input := muxSample(t, data, sample, packets)
		err := mux.Process(t.Context(), flow.NewSelectedBatch(0, &input), &muxWriteCollector{})
		if !errors.Is(err, ErrUnsupported) || input.Valid() || packets.Used() != 0 {
			t.Fatalf("mutated Process = %v input=%t used=%d", err, input.Valid(), packets.Used())
		}
	})

	t.Run("track return", func(t *testing.T) {
		localJournal, err := scratch.Open(compiled.Scratch())
		if err != nil {
			t.Fatal(err)
		}
		defer localJournal.Close()
		mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), buffers, localJournal)
		collector := &muxWriteCollector{}
		first := muxSample(t, data, movieSamples(t, data, inspected, 0)[0], packets)
		if err := mux.Process(t.Context(), flow.NewSelectedBatch(0, &first), collector); err != nil {
			t.Fatal(err)
		}
		returned := muxSample(t, data, movieSamples(t, data, inspected, 0)[0], packets)
		err = mux.Process(t.Context(), flow.NewSelectedBatch(0, &returned), collector)
		if !errors.Is(err, ErrUnsupported) || returned.Valid() {
			t.Fatalf("returned track Process = %v input=%t", err, returned.Valid())
		}
		for _, item := range collector.items {
			item.Drop()
		}
	})
}

func TestMP4MuxFlushFailsClosedForTruncatedAndCanceledSource(t *testing.T) {
	data := append(twoTrackMovie(true, "isom", "iso2"), fixtureBox("free", []byte{1})...)
	inspected := inspectMovie(t, data)
	component, compiled := compileMP4Mux(t, inspected)
	packets := mustMP4Allocator(t, 8)

	t.Run("truncated suffix", func(t *testing.T) {
		buffers := mustMP4Allocator(t, 1<<20)
		journal, err := scratch.Open(compiled.Scratch())
		if err != nil {
			t.Fatal(err)
		}
		defer journal.Close()
		mux := openMP4Mux(t, component, compiled, movieSourceOpeningWithSize(t, data[:len(data)-1], int64(len(data))), buffers, journal)
		collector := &muxWriteCollector{}
		for ordinal := range inspected.tracks {
			input := muxSample(t, data, movieSamples(t, data, inspected, ordinal)[0], packets)
			if err := mux.Process(t.Context(), flow.NewSelectedBatch(ordinal, &input), collector); err != nil {
				t.Fatal(err)
			}
		}
		if err := mux.Finalize(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := mux.Flush(t.Context(), collector); !errors.Is(err, ErrTruncated) {
			t.Fatalf("truncated Flush = %v", err)
		}
		for _, item := range collector.items {
			item.Drop()
		}
	})

	t.Run("canceled process", func(t *testing.T) {
		buffers := mustMP4Allocator(t, 1<<20)
		journal, err := scratch.Open(compiled.Scratch())
		if err != nil {
			t.Fatal(err)
		}
		defer journal.Close()
		mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), buffers, journal)
		input := muxSample(t, data, movieSamples(t, data, inspected, 0)[0], packets)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err = mux.Process(ctx, flow.NewSelectedBatch(0, &input), &muxWriteCollector{})
		if !errors.Is(err, context.Canceled) || input.Valid() {
			t.Fatalf("canceled Process = %v input=%t", err, input.Valid())
		}
	})
}

func TestMP4MuxPreflightsSTCOOverflowBeforeAppendingSuffix(t *testing.T) {
	journal := &muxMemoryScratch{data: make([]byte, 8)}
	binary.BigEndian.PutUint64(journal.data, uint64(^uint32(0))+1)
	mux := &muxer{
		layout:         muxLayout{tracks: []muxTrack{{value: track{chunkCount: 1}}}},
		buffers:        mustMP4Allocator(t, muxJournalPageBytes),
		scratch:        journal,
		need:           8,
		scratchWritten: 8,
	}
	if err := mux.preflightOffsets(t.Context()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("stco preflight = %v", err)
	}
}

func TestMP4MuxFailsClosedForJournalFailure(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	inspected := inspectMovie(t, data)
	component, compiled := compileMP4Mux(t, inspected)
	packets := mustMP4Allocator(t, 8)

	t.Run("append cancellation", func(t *testing.T) {
		journal := &muxMemoryScratch{appendErr: context.Canceled}
		mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), mustMP4Allocator(t, 1<<20), journal)
		collector := &muxWriteCollector{}
		for ordinal := range inspected.tracks {
			input := muxSample(t, data, movieSamples(t, data, inspected, ordinal)[0], packets)
			if err := mux.Process(t.Context(), flow.NewSelectedBatch(ordinal, &input), collector); err != nil {
				t.Fatalf("journal append Process = %v", err)
			}
		}
		if err := mux.Finalize(t.Context()); !errors.Is(err, context.Canceled) {
			t.Fatalf("journal append Finalize = %v", err)
		}
		for _, item := range collector.items {
			item.Drop()
		}
	})

	t.Run("read poison", func(t *testing.T) {
		journal := &muxMemoryScratch{readErr: errors.New("journal read failed")}
		mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), mustMP4Allocator(t, 1<<20), journal)
		collector := &muxWriteCollector{}
		for ordinal := range inspected.tracks {
			input := muxSample(t, data, movieSamples(t, data, inspected, ordinal)[0], packets)
			if err := mux.Process(t.Context(), flow.NewSelectedBatch(ordinal, &input), collector); err != nil {
				t.Fatal(err)
			}
		}
		if err := mux.Finalize(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := mux.Flush(t.Context(), collector); !errors.Is(err, journal.readErr) {
			t.Fatalf("journal read Flush = %v", err)
		}
		for _, item := range collector.items {
			item.Drop()
		}
	})

	t.Run("short read", func(t *testing.T) {
		journal := &muxMemoryScratch{readErr: io.ErrUnexpectedEOF}
		mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), mustMP4Allocator(t, 1<<20), journal)
		collector := &muxWriteCollector{}
		for ordinal := range inspected.tracks {
			input := muxSample(t, data, movieSamples(t, data, inspected, ordinal)[0], packets)
			if err := mux.Process(t.Context(), flow.NewSelectedBatch(ordinal, &input), collector); err != nil {
				t.Fatal(err)
			}
		}
		if err := mux.Finalize(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := mux.Flush(t.Context(), collector); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("journal short Flush = %v", err)
		}
		for _, item := range collector.items {
			item.Drop()
		}
	})
}

func TestMP4MuxCloseDropsBorrowedSourceAndScratch(t *testing.T) {
	data := twoTrackMovie(false, "isom", "iso2")
	inspected := inspectMovie(t, data)
	component, compiled := compileMP4Mux(t, inspected)
	journal, err := scratch.Open(compiled.Scratch())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	mux := openMP4Mux(t, component, compiled, movieSourceOpening(t, data), mustMP4Allocator(t, muxPageBytes), journal)
	if err := mux.Close(); err != nil {
		t.Fatal(err)
	}
	if mux.reader != nil || mux.buffers != nil || mux.scratch != nil || mux.cursor.reader != nil {
		t.Fatal("MP4 mux Close retained a borrowed dependency")
	}
}
