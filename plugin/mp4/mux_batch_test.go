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
	appendErr    error
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

func (s *muxBatchScratchSpy) WriteAt(_ context.Context, value []byte, offset int64) error {
	if offset < 0 || int64(len(value)) > int64(len(s.data))-offset {
		return errors.New("scratch write outside test extent")
	}
	copy(s.data[offset:], value)
	return nil
}

func TestMP4MuxBatchesChunkOffsetScratchAppends(t *testing.T) {
	const chunks = 10_000
	const recordBytes = 8
	spy := &muxBatchScratchSpy{}
	mux := &muxer{movie: movie{}, scratch: spy, need: chunks * recordBytes}

	for index := 0; index < chunks; index++ {
		mux.outputOffset = uint64(index * 1024)
		if err := mux.appendChunkOffset(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if mux.scratchPageUsed != 784*recordBytes {
		t.Fatalf("partial page used = %d, want %d", mux.scratchPageUsed, 784*recordBytes)
	}
	if mux.scratchWritten != 9*muxJournalPageBytes {
		t.Fatalf("scratch written = %d, want %d", mux.scratchWritten, 9*muxJournalPageBytes)
	}
	if len(spy.appendSizes) != 9 {
		t.Fatalf("full page append count = %d, want 9", len(spy.appendSizes))
	}
	for index, size := range spy.appendSizes {
		if size != muxJournalPageBytes {
			t.Fatalf("append %d size = %d, want %d", index, size, muxJournalPageBytes)
		}
	}

	if err := mux.Finalize(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(spy.appendSizes) != 10 || spy.appendSizes[9] != 784*recordBytes {
		t.Fatalf("final append sizes = %v, want nine %d-byte pages and %d bytes", spy.appendSizes, muxJournalPageBytes, 784*recordBytes)
	}
	if mux.scratchPageUsed != 0 || mux.scratchWritten != chunks*recordBytes {
		t.Fatalf("final scratch state = used %d written %d", mux.scratchPageUsed, mux.scratchWritten)
	}
	for index := 0; index < chunks; index++ {
		got := binary.BigEndian.Uint64(spy.data[index*recordBytes : index*recordBytes+recordBytes])
		want := uint64(index * 1024)
		if got != want {
			t.Fatalf("journal offset %d = %d, want %d", index, got, want)
		}
	}
}

func TestMP4MuxChunkOffsetPageFailureDoesNotAdvanceState(t *testing.T) {
	t.Run("append error", func(t *testing.T) {
		wantErr := errors.New("scratch append failed")
		spy := &muxBatchScratchSpy{appendErr: wantErr}
		mux := &muxer{scratch: spy, need: muxJournalPageBytes, scratchPageUsed: muxJournalPageBytes - 8}
		if err := mux.appendChunkOffset(t.Context()); !errors.Is(err, wantErr) {
			t.Fatalf("append error = %v", err)
		}
		if mux.scratchPageUsed != muxJournalPageBytes-8 || mux.scratchWritten != 0 {
			t.Fatalf("failed append advanced state: used %d written %d", mux.scratchPageUsed, mux.scratchWritten)
		}
	})

	t.Run("offset mismatch", func(t *testing.T) {
		spy := &muxBatchScratchSpy{returnedBase: 1}
		mux := &muxer{scratch: spy, need: muxJournalPageBytes, scratchPageUsed: muxJournalPageBytes - 8}
		if err := mux.appendChunkOffset(t.Context()); !errors.Is(err, ErrMalformed) {
			t.Fatalf("offset mismatch = %v", err)
		}
		if mux.scratchPageUsed != muxJournalPageBytes-8 || mux.scratchWritten != 0 {
			t.Fatalf("mismatched append advanced state: used %d written %d", mux.scratchPageUsed, mux.scratchWritten)
		}
	})

	t.Run("finalize cancellation", func(t *testing.T) {
		spy := &muxBatchScratchSpy{}
		mux := &muxer{movie: movie{}, scratch: spy, need: 8}
		if err := mux.appendChunkOffset(t.Context()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := mux.Finalize(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Finalize = %v", err)
		}
		if mux.scratchPageUsed != 8 || mux.scratchWritten != 0 || len(spy.appendSizes) != 0 {
			t.Fatalf("canceled finalize advanced state: used %d written %d appends %v", mux.scratchPageUsed, mux.scratchWritten, spy.appendSizes)
		}
	})
}

func TestMP4MuxChunkOffsetCardinalityAndEmptyJournal(t *testing.T) {
	t.Run("cardinality", func(t *testing.T) {
		spy := &muxBatchScratchSpy{}
		mux := &muxer{movie: movie{}, scratch: spy, need: 16}
		if err := mux.appendChunkOffset(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := mux.Finalize(t.Context()); !errors.Is(err, ErrMalformed) {
			t.Fatalf("incomplete journal Finalize = %v", err)
		}
		if mux.scratchPageUsed != 0 || mux.scratchWritten != 8 || len(spy.appendSizes) != 1 || spy.appendSizes[0] != 8 {
			t.Fatalf("incomplete journal state: used %d written %d appends %v", mux.scratchPageUsed, mux.scratchWritten, spy.appendSizes)
		}
	})

	t.Run("empty", func(t *testing.T) {
		mux := &muxer{movie: movie{}, need: 0}
		if err := mux.Finalize(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}

func BenchmarkMP4MuxChunkOffsetScratchBatch(b *testing.B) {
	const chunks = 10_000
	const recordBytes = 8
	spy := &muxBatchScratchSpy{
		data:        make([]byte, 0, chunks*recordBytes),
		appendSizes: make([]int, 0, (chunks*recordBytes+muxJournalPageBytes-1)/muxJournalPageBytes),
	}
	b.SetBytes(chunks * recordBytes)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		spy.data = spy.data[:0]
		spy.appendSizes = spy.appendSizes[:0]
		mux := &muxer{movie: movie{}, scratch: spy, need: chunks * recordBytes}
		for chunk := 0; chunk < chunks; chunk++ {
			mux.outputOffset = uint64(chunk * 1024)
			if err := mux.appendChunkOffset(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
		if err := mux.Finalize(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

var _ plugin.Scratch = (*muxBatchScratchSpy)(nil)
