package scratch

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"sync"
	"testing"

	"github.com/godexture/godec/resource"
)

func TestReserveRejectsDisabledLimitAndOverflow(t *testing.T) {
	if _, err := Reserve(0, 1); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled reservation error = %v", err)
	}
	if _, err := Reserve(resource.Bytes(math.MaxInt64), resource.Bytes(math.MaxInt64), 1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflow reservation error = %v", err)
	}
	if _, err := Reserve(3, 4); !errors.Is(err, ErrLimit) {
		t.Fatalf("limited reservation error = %v", err)
	}
}

func TestJournalAppendReadPatchAndExtent(t *testing.T) {
	journal, err := Open(8)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if offset, err := journal.Append(t.Context(), []byte("abcdef")); err != nil || offset != 0 {
		t.Fatalf("append offset = %d, %v", offset, err)
	}
	if err := journal.WriteAt(t.Context(), []byte("XY"), 2); err != nil {
		t.Fatalf("patch = %v", err)
	}
	got := make([]byte, 6)
	if err := journal.ReadAt(t.Context(), got, 0); err != nil || string(got) != "abXYef" {
		t.Fatalf("read = %q, %v", got, err)
	}
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "read past extent", err: journal.ReadAt(t.Context(), make([]byte, 1), 6)},
		{name: "patch extends extent", err: journal.WriteAt(t.Context(), []byte("!"), 6)},
		{name: "patch hole", err: journal.WriteAt(t.Context(), []byte("!"), -1)},
	} {
		if !errors.Is(test.err, ErrExtent) {
			t.Fatalf("%s error = %v", test.name, test.err)
		}
	}
	if count, err := journal.Append(t.Context(), []byte("123")); count != 0 || !errors.Is(err, ErrQuota) {
		t.Fatalf("over-quota append = %d, %v", count, err)
	}
}

func TestJournalSerializesConcurrentAppendAndDeletesOnClose(t *testing.T) {
	journal, err := Open(128)
	if err != nil {
		t.Fatal(err)
	}
	path := journal.temporary.path
	const writers = 32
	var group sync.WaitGroup
	errs := make(chan error, writers)
	offsets := make(chan int64, writers)
	for index := 0; index < writers; index++ {
		group.Add(1)
		go func(value byte) {
			defer group.Done()
			offset, appendErr := journal.Append(context.Background(), []byte{value})
			if appendErr != nil || offset < 0 || offset >= writers {
				errs <- appendErr
				return
			}
			offsets <- offset
		}(byte(index))
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	seenOffsets := make(map[int64]bool, writers)
	close(offsets)
	for offset := range offsets {
		seenOffsets[offset] = true
	}
	if len(seenOffsets) != writers {
		t.Fatalf("concurrent append returned %d unique offsets", len(seenOffsets))
	}
	values := make([]byte, writers)
	if err := journal.ReadAt(t.Context(), values, 0); err != nil {
		t.Fatal(err)
	}
	seen := make(map[byte]bool, writers)
	for _, value := range values {
		seen[value] = true
	}
	if len(seen) != writers {
		t.Fatalf("concurrent append retained %d values", len(seen))
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary scratch file remained after Close: %v", err)
	}
	if err := journal.WriteAt(t.Context(), nil, 0); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after close = %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("second close = %v", err)
	}
}

func TestJournalHonorsCancelledContext(t *testing.T) {
	journal, err := Open(4)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("cancel scratch")
	cancel(cause)
	if offset, err := journal.Append(ctx, []byte("x")); offset != 0 || !errors.Is(err, cause) {
		t.Fatalf("cancelled append = %d, %v", offset, err)
	}
}

func TestJournalPoisonRejectsEveryLaterOperation(t *testing.T) {
	journal, err := Open(4)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	cause := errors.New("temporary write failed")
	journal.mu.Lock()
	journal.poison(cause)
	journal.mu.Unlock()
	if _, err := journal.Append(t.Context(), []byte("x")); !errors.Is(err, cause) {
		t.Fatalf("append after poison = %v", err)
	}
	if err := journal.ReadAt(t.Context(), nil, 0); !errors.Is(err, cause) {
		t.Fatalf("read after poison = %v", err)
	}
	if err := journal.WriteAt(t.Context(), nil, 0); !errors.Is(err, cause) {
		t.Fatalf("write after poison = %v", err)
	}
}

func TestJournalPoisonPrecedesCancelledContext(t *testing.T) {
	journal, err := Open(4)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	poison := errors.New("write error")
	journal.mu.Lock()
	journal.poison(poison)
	journal.mu.Unlock()
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("caller cancelled"))
	if _, err := journal.Append(ctx, []byte("x")); !errors.Is(err, poison) {
		t.Fatalf("append poison precedence = %v", err)
	}
	if err := journal.ReadAt(ctx, nil, 0); !errors.Is(err, poison) {
		t.Fatalf("read poison precedence = %v", err)
	}
	if err := journal.WriteAt(ctx, nil, 0); !errors.Is(err, poison) {
		t.Fatalf("write poison precedence = %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(ctx, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed precedence = %v", err)
	}
}

type partialJournalFile struct {
	writes int
}

func (f *partialJournalFile) ReadAt([]byte, int64) (int, error) { return 0, nil }
func (f *partialJournalFile) WriteAt(source []byte, _ int64) (int, error) {
	f.writes++
	return len(source) - 1, nil
}
func (*partialJournalFile) Close() error { return nil }

func TestPartialAppendPoisonsWithoutAdvancingExtent(t *testing.T) {
	file := &partialJournalFile{}
	journal := &Journal{file: file, maximum: 8}
	if offset, err := journal.Append(t.Context(), []byte("abc")); offset != 0 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("partial append = %d, %v", offset, err)
	}
	if journal.extent != 0 {
		t.Fatalf("partial append advanced extent to %d", journal.extent)
	}
	if _, err := journal.Append(t.Context(), []byte("x")); !errors.Is(err, io.ErrShortWrite) || file.writes != 1 {
		t.Fatalf("append after partial = %v, writes=%d", err, file.writes)
	}
}

type cancelJournalFile struct{ cancel context.CancelCauseFunc }

func (f cancelJournalFile) ReadAt([]byte, int64) (int, error) { return 0, nil }
func (f cancelJournalFile) WriteAt(source []byte, _ int64) (int, error) {
	f.cancel(nil)
	return len(source), nil
}
func (cancelJournalFile) Close() error { return nil }

func TestAppendCancelledAfterWritePoisonsWithoutAdvancingExtent(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	journal := &Journal{file: cancelJournalFile{cancel: cancel}, maximum: 8}
	if offset, err := journal.Append(ctx, []byte("x")); offset != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled append = %d, %v", offset, err)
	}
	if journal.extent != 0 {
		t.Fatalf("cancelled append advanced extent to %d", journal.extent)
	}
	if _, err := journal.Append(t.Context(), []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("append after cancelled write = %v", err)
	}
}
