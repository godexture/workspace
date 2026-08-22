package mp4

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/godexture/godec/plugin"
)

type muxBatchScratchSpy struct {
	data         []byte
	appendSizes  []int
	writeSizes   []int
	appendErr    error
	writeErr     error
	returnedBase int64
}

func (s *muxBatchScratchSpy) Append(ctx context.Context, value []byte) (int64, error) {
	s.appendSizes = append(s.appendSizes, len(value))
	if s.appendErr != nil {
		return 0, s.appendErr
	}
	if err := context.Cause(ctx); err != nil {
		return 0, err
	}
	offset := int64(len(s.data)) + s.returnedBase
	s.data = append(s.data, value...)
	return offset, nil
}

func (s *muxBatchScratchSpy) ReadAt(_ context.Context, target []byte, offset int64) error {
	if offset < 0 || int64(len(target)) > int64(len(s.data))-offset {
		return errors.New("scratch read outside test extent")
	}
	copy(target, s.data[offset:])
	return nil
}

func (s *muxBatchScratchSpy) WriteAt(ctx context.Context, value []byte, offset int64) error {
	s.writeSizes = append(s.writeSizes, len(value))
	if s.writeErr != nil {
		return s.writeErr
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if offset < 0 || int64(len(value)) > int64(len(s.data))-offset {
		return errors.New("scratch write outside test extent")
	}
	copy(s.data[offset:], value)
	return nil
}

// journalOnlyMuxer drives the chunk-offset journal without a movie. Finalize
// reads the payload size, each track's sample count and each track's chunk
// count, so tracks that describe chunks but no samples are enough to exercise
// the journal on its own.
func journalOnlyMuxer(journal plugin.Scratch, chunks ...uint32) *muxer {
	layout := muxLayout{pieces: []muxPiece{{kind: muxCopy}, {kind: muxPayload}}, payload: 1}
	for _, count := range chunks {
		layout.tracks = append(layout.tracks, muxTrack{value: track{chunkCount: count}})
	}
	if err := layout.placeJournal(); err != nil {
		panic(err)
	}
	need := layout.journalBytes()
	return &muxer{
		layout:  layout,
		scratch: journal,
		need:    int64(need),
		tracks:  make([]muxCursor, len(layout.tracks)),
	}
}

func TestMP4MuxBatchesChunkOffsetScratchWrites(t *testing.T) {
	const chunks = 10_000
	const recordBytes = 8
	const perPage = muxJournalTrackPageBytes / recordBytes
	spy := &muxBatchScratchSpy{}
	mux := journalOnlyMuxer(spy, chunks)
	if err := mux.sizeJournal(t.Context()); err != nil {
		t.Fatal(err)
	}
	// The journal exists in full before any track records into it, because the
	// tracks fill their own regions rather than appending in turn.
	if len(spy.data) != chunks*recordBytes {
		t.Fatalf("sized journal = %d bytes, want %d", len(spy.data), chunks*recordBytes)
	}
	if len(spy.writeSizes) != 0 {
		t.Fatalf("sizing the journal wrote %v", spy.writeSizes)
	}

	for index := range chunks {
		mux.outputOffset = uint64(index * 1024)
		if err := mux.recordChunkOffset(t.Context(), 0); err != nil {
			t.Fatal(err)
		}
	}
	if want := chunks / perPage; len(spy.writeSizes) != want {
		t.Fatalf("full page writes = %d, want %d", len(spy.writeSizes), want)
	}
	if mux.tracks[0].used != (chunks%perPage)*recordBytes {
		t.Fatalf("partial page used = %d, want %d", mux.tracks[0].used, (chunks%perPage)*recordBytes)
	}

	if err := mux.Finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if mux.tracks[0].used != 0 || mux.tracks[0].recorded != chunks {
		t.Fatalf("final track state = used %d recorded %d", mux.tracks[0].used, mux.tracks[0].recorded)
	}
	for index := range chunks {
		got := binary.BigEndian.Uint64(spy.data[index*recordBytes : index*recordBytes+recordBytes])
		if want := uint64(index * 1024); got != want {
			t.Fatalf("journal offset %d = %d, want %d", index, got, want)
		}
	}
}

// TestMP4MuxKeepsInterleavedChunkOffsetsInTrackRegions is what the per-track
// regions exist for. Chunks arrive interleaved, so an append-only journal would
// mix the tracks together; each entry has to land in its own track's run, in
// chunk order, for the patch phase to read one table as a sequence.
func TestMP4MuxKeepsInterleavedChunkOffsetsInTrackRegions(t *testing.T) {
	const chunks = 3
	spy := &muxBatchScratchSpy{}
	mux := journalOnlyMuxer(spy, chunks, chunks)
	if err := mux.sizeJournal(t.Context()); err != nil {
		t.Fatal(err)
	}
	for index := range chunks {
		for ordinal := range 2 {
			mux.outputOffset = uint64(index*100 + ordinal*10)
			if err := mux.recordChunkOffset(t.Context(), ordinal); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := mux.Finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	for ordinal := range 2 {
		base := mux.layout.tracks[ordinal].journal
		if want := uint64(ordinal * chunks * 8); base != want {
			t.Fatalf("track %d journal region starts at %d, want %d", ordinal, base, want)
		}
		for index := range chunks {
			position := base + uint64(index*8)
			got := binary.BigEndian.Uint64(spy.data[position : position+8])
			if want := uint64(index*100 + ordinal*10); got != want {
				t.Fatalf("track %d chunk %d recorded %d, want %d", ordinal, index, got, want)
			}
		}
	}
}

func TestMP4MuxChunkOffsetPageFailureDoesNotAdvanceState(t *testing.T) {
	t.Run("sizing error", func(t *testing.T) {
		wantErr := errors.New("scratch append failed")
		spy := &muxBatchScratchSpy{appendErr: wantErr}
		mux := journalOnlyMuxer(spy, 1)
		if err := mux.sizeJournal(t.Context()); !errors.Is(err, wantErr) {
			t.Fatalf("sizing error = %v", err)
		}
		if mux.sized {
			t.Fatal("a failed sizing left the journal marked ready")
		}
		if err := mux.recordChunkOffset(t.Context(), 0); !errors.Is(err, ErrMalformed) {
			t.Fatalf("recording into an unsized journal = %v", err)
		}
	})

	t.Run("sizing offset mismatch", func(t *testing.T) {
		spy := &muxBatchScratchSpy{returnedBase: 1}
		mux := journalOnlyMuxer(spy, 1)
		if err := mux.sizeJournal(t.Context()); !errors.Is(err, ErrMalformed) {
			t.Fatalf("offset mismatch = %v", err)
		}
	})

	t.Run("write error", func(t *testing.T) {
		const perPage = muxJournalTrackPageBytes / 8
		wantErr := errors.New("scratch write failed")
		spy := &muxBatchScratchSpy{}
		mux := journalOnlyMuxer(spy, perPage)
		if err := mux.sizeJournal(t.Context()); err != nil {
			t.Fatal(err)
		}
		spy.writeErr = wantErr
		var err error
		for index := 0; index < perPage && err == nil; index++ {
			err = mux.recordChunkOffset(t.Context(), 0)
		}
		if !errors.Is(err, wantErr) {
			t.Fatalf("page write error = %v", err)
		}
		// The page keeps its bytes so a retry would not lose an entry, and the
		// journal keeps its zeros so no partial run is mistaken for offsets.
		if mux.tracks[0].used != muxJournalTrackPageBytes {
			t.Fatalf("failed write emptied the page: used %d", mux.tracks[0].used)
		}
		for index, value := range spy.data {
			if value != 0 {
				t.Fatalf("failed write left byte %d as %d", index, value)
			}
		}
	})

	t.Run("finalize cancellation", func(t *testing.T) {
		spy := &muxBatchScratchSpy{}
		mux := journalOnlyMuxer(spy, 1)
		if err := mux.sizeJournal(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := mux.recordChunkOffset(t.Context(), 0); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := mux.Finalize(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Finalize = %v", err)
		}
		if mux.tracks[0].used != 8 || len(spy.writeSizes) != 0 {
			t.Fatalf("canceled finalize advanced state: used %d writes %v", mux.tracks[0].used, spy.writeSizes)
		}
	})
}

func TestMP4MuxChunkOffsetCardinalityAndEmptyJournal(t *testing.T) {
	t.Run("cardinality", func(t *testing.T) {
		spy := &muxBatchScratchSpy{}
		mux := journalOnlyMuxer(spy, 2)
		if err := mux.sizeJournal(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := mux.recordChunkOffset(t.Context(), 0); err != nil {
			t.Fatal(err)
		}
		if err := mux.Finalize(t.Context()); !errors.Is(err, ErrMalformed) {
			t.Fatalf("incomplete journal Finalize = %v", err)
		}
	})

	t.Run("beyond the track's chunks", func(t *testing.T) {
		spy := &muxBatchScratchSpy{}
		mux := journalOnlyMuxer(spy, 1)
		if err := mux.sizeJournal(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := mux.recordChunkOffset(t.Context(), 0); err != nil {
			t.Fatal(err)
		}
		if err := mux.recordChunkOffset(t.Context(), 0); !errors.Is(err, ErrMalformed) {
			t.Fatalf("recording a chunk the track does not have = %v", err)
		}
	})

	t.Run("empty", func(t *testing.T) {
		mux := journalOnlyMuxer(nil)
		if err := mux.sizeJournal(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := mux.Finalize(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

func BenchmarkMP4MuxChunkOffsetScratchBatch(b *testing.B) {
	const chunks = 10_000
	const recordBytes = 8
	spy := &muxBatchScratchSpy{
		data:       make([]byte, 0, chunks*recordBytes),
		writeSizes: make([]int, 0, chunks*recordBytes/muxJournalTrackPageBytes+1),
	}
	b.SetBytes(chunks * recordBytes)
	b.ReportAllocs()
	for range b.N {
		spy.data = spy.data[:0]
		spy.appendSizes = spy.appendSizes[:0]
		spy.writeSizes = spy.writeSizes[:0]
		mux := journalOnlyMuxer(spy, chunks)
		if err := mux.sizeJournal(context.Background()); err != nil {
			b.Fatal(err)
		}
		for chunk := range chunks {
			mux.outputOffset = uint64(chunk * 1024)
			if err := mux.recordChunkOffset(context.Background(), 0); err != nil {
				b.Fatal(err)
			}
		}
		if err := mux.Finalize(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

var _ plugin.Scratch = (*muxBatchScratchSpy)(nil)
