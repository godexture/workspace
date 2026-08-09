package host

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/godexture/godec/access"
)

type spoolSinkFixture struct {
	data      []byte
	limit     int
	writeErr  error
	commitErr error
	events    []string
}

func (*spoolSinkFixture) Capabilities() access.Capabilities {
	value, _ := access.NewCapabilities(access.SequentialWrite)
	return value
}

func (s *spoolSinkFixture) Write(_ context.Context, source []byte) (int, error) {
	s.events = append(s.events, "write")
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	size := len(source)
	if s.limit > 0 {
		size = min(size, s.limit)
	}
	s.data = append(s.data, source[:size]...)
	return size, nil
}

func (s *spoolSinkFixture) Flush(context.Context) error {
	s.events = append(s.events, "flush")
	return nil
}
func (s *spoolSinkFixture) Sync(context.Context) error {
	s.events = append(s.events, "sync")
	return nil
}
func (s *spoolSinkFixture) PrepareCommit(context.Context) error {
	s.events = append(s.events, "prepare")
	return nil
}
func (s *spoolSinkFixture) Commit(context.Context) error {
	s.events = append(s.events, "commit")
	return s.commitErr
}
func (s *spoolSinkFixture) Abort(context.Context) error {
	s.events = append(s.events, "abort")
	return nil
}
func (s *spoolSinkFixture) Close() error {
	s.events = append(s.events, "close")
	return nil
}

type spoolStorageFixture struct {
	data     []byte
	writeErr error
	writes   int
	closed   int
}

func (s *spoolStorageFixture) WriteAt(source []byte, offset int64) (int, error) {
	s.writes++
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	end := int(offset) + len(source)
	if end > len(s.data) {
		s.data = append(s.data, make([]byte, end-len(s.data))...)
	}
	copy(s.data[int(offset):end], source)
	return len(source), nil
}

func (s *spoolStorageFixture) CopyTo(ctx context.Context, destination access.Appender, extent int64) error {
	return appendAll(ctx, destination, s.data[:int(extent)])
}

func (s *spoolStorageFixture) Close() error {
	s.closed++
	s.data = nil
	return nil
}

func TestSpoolCopiesPositionedBytesOnceBeforeCommit(t *testing.T) {
	for _, storage := range []access.SpoolStorage{access.MemorySpool, access.DiskSpool} {
		t.Run(spoolStorageName(storage), func(t *testing.T) {
			underlying := &spoolSinkFixture{limit: 2}
			session := openedSpool(t, storage, 32, underlying)
			var diskPath string
			if disk, ok := session.storage.(*diskSpool); ok {
				diskPath = disk.path
			}
			if count, err := session.WriteAt(t.Context(), []byte("abcdef"), 0); err != nil || count != 6 {
				t.Fatalf("append to spool = %d, %v", count, err)
			}
			if count, err := session.WriteAt(t.Context(), []byte("XY"), 2); err != nil || count != 2 {
				t.Fatalf("patch spool = %d, %v", count, err)
			}
			if len(underlying.data) != 0 {
				t.Fatal("spool published bytes before Flush")
			}
			if err := session.Flush(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := string(underlying.data); got != "abXYef" {
				t.Fatalf("final copy = %q", got)
			}
			if diskPath != "" {
				if _, err := os.Stat(diskPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("disk spool remains after final copy: %v", err)
				}
			}
			if err := session.Sync(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := session.PrepareCommit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := session.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
			if !equalStrings(underlying.events, []string{"write", "write", "write", "flush", "sync", "prepare", "commit", "close"}) {
				t.Fatalf("spool lifecycle = %v", underlying.events)
			}
		})
	}
}

func TestSpoolQuotaAndCancellationFailBeforeStorageWrite(t *testing.T) {
	underlying := &spoolSinkFixture{}
	storage := &spoolStorageFixture{}
	session := openedSpoolWithStorage(t, 4, underlying, storage)
	if _, err := session.WriteAt(t.Context(), []byte("four"), 1); !errors.Is(err, errSpoolLimit) || storage.writes != 0 {
		t.Fatalf("quota failure = %v, writes %d", err, storage.writes)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := session.WriteAt(ctx, []byte{1}, 0); !errors.Is(err, context.Canceled) || storage.writes != 0 {
		t.Fatalf("cancel failure = %v, writes %d", err, storage.writes)
	}
	if err := session.Abort(t.Context()); err != nil {
		t.Fatal(err)
	}
	if storage.closed == 0 {
		t.Fatal("aborted spool did not close storage")
	}
}

func TestSpoolCleansStorageAfterStorageCopyAndCommitFailures(t *testing.T) {
	tests := []struct {
		name      string
		storage   *spoolStorageFixture
		configure func(*spoolSinkFixture)
		phase     func(*spoolSession) error
	}{
		{
			name:    "storage write",
			storage: &spoolStorageFixture{writeErr: errors.New("storage failed")},
			phase: func(session *spoolSession) error {
				_, err := session.WriteAt(t.Context(), []byte("data"), 0)
				return err
			},
		},
		{
			name:      "final copy",
			storage:   &spoolStorageFixture{},
			configure: func(sink *spoolSinkFixture) { sink.writeErr = errors.New("copy failed") },
			phase: func(session *spoolSession) error {
				_, _ = session.WriteAt(t.Context(), []byte("data"), 0)
				return session.Flush(t.Context())
			},
		},
		{
			name:      "commit",
			storage:   &spoolStorageFixture{},
			configure: func(sink *spoolSinkFixture) { sink.commitErr = errors.New("commit failed") },
			phase: func(session *spoolSession) error {
				_, _ = session.WriteAt(t.Context(), []byte("data"), 0)
				if err := session.Flush(t.Context()); err != nil {
					return err
				}
				if err := session.Sync(t.Context()); err != nil {
					return err
				}
				if err := session.PrepareCommit(t.Context()); err != nil {
					return err
				}
				return session.Commit(t.Context())
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			underlying := &spoolSinkFixture{}
			if test.configure != nil {
				test.configure(underlying)
			}
			session := openedSpoolWithStorage(t, 16, underlying, test.storage)
			if err := test.phase(session); err == nil {
				t.Fatal("injected spool failure was not returned")
			}
			if err := session.Abort(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := session.Close(); err != nil {
				t.Fatal(err)
			}
			if test.storage.closed == 0 || !slices.Contains(underlying.events, "abort") {
				t.Fatalf("failure cleanup = storage %d, events %v", test.storage.closed, underlying.events)
			}
		})
	}
}

func openedSpool(t *testing.T, storage access.SpoolStorage, maximum int64, underlying *spoolSinkFixture) *spoolSession {
	t.Helper()
	spec, err := access.NewSpoolSpec(maximum, 0, storage, 0, true, access.AtomicReplace)
	if err != nil {
		t.Fatal(err)
	}
	opening := sequentialOpening(t, underlying)
	result, err := newSpoolSession(spec, underlying, opening)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func openedSpoolWithStorage(t *testing.T, maximum int64, underlying *spoolSinkFixture, storage spoolStorage) *spoolSession {
	t.Helper()
	spec, err := access.NewSpoolSpec(maximum, 0, access.MemorySpool, 0, true, access.AtomicReplace)
	if err != nil {
		t.Fatal(err)
	}
	result, err := newSpoolSessionWithStorage(spec, underlying, sequentialOpening(t, underlying), storage)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func sequentialOpening(t *testing.T, session access.Session) access.Opening {
	t.Helper()
	available, err := access.NewCapabilities(access.SequentialWrite)
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := access.Select(available, access.NewRequirements(access.AnyOf(access.SequentialWrite)))
	if !ok {
		t.Fatal("sequential write selection failed")
	}
	opening, err := access.NewOpening(access.SinkDirection, session, selected, access.AtomicReplace)
	if err != nil {
		t.Fatal(err)
	}
	return opening
}

func spoolStorageName(value access.SpoolStorage) string {
	if value == access.MemorySpool {
		return "memory"
	}
	return "disk"
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
