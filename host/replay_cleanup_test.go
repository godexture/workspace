package host

import (
	"context"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/access"
)

type panicReplaySession struct {
	closed atomic.Int32
}

func (s *panicReplaySession) Capabilities() access.Capabilities { return access.Capabilities{} }

func (*panicReplaySession) Read(context.Context, []byte) (int, error) { return 0, io.EOF }

func (s *panicReplaySession) Close() error {
	s.closed.Add(1)
	panic("replay-close-secret")
}

func TestReplayCloseRetainsPanicErrorAndClosesUnderlyingOnce(t *testing.T) {
	underlying := &panicReplaySession{}
	replayed := &replaySession{underlying: underlying, reader: underlying}

	err := replayed.Close()
	if err == nil || strings.Contains(err.Error(), "replay-close-secret") {
		t.Fatalf("first Close error = %v, want a redacted panic error", err)
	}
	if underlying.closed.Load() != 1 {
		t.Fatalf("underlying Close calls = %d, want 1", underlying.closed.Load())
	}
	second := replayed.Close()
	if second == nil || second.Error() != err.Error() {
		t.Fatalf("second Close error = %v, want retained first error %v", second, err)
	}
	if underlying.closed.Load() != 1 {
		t.Fatalf("underlying Close calls after second Close = %d, want 1", underlying.closed.Load())
	}
}

type concurrentReplaySource struct {
	caps     access.Capabilities
	snapshot access.Snapshot
	closed   atomic.Int32
	calls    atomic.Int32
}

func (s *concurrentReplaySource) Capabilities() access.Capabilities {
	s.calls.Add(1)
	runtime.Gosched()
	return s.caps
}

func (s *concurrentReplaySource) Read(context.Context, []byte) (int, error) {
	s.calls.Add(1)
	runtime.Gosched()
	return 0, io.EOF
}

func (s *concurrentReplaySource) Size(context.Context) (int64, error) {
	s.calls.Add(1)
	runtime.Gosched()
	return 0, nil
}

func (s *concurrentReplaySource) Snapshot(context.Context) (access.Snapshot, error) {
	s.calls.Add(1)
	runtime.Gosched()
	return s.snapshot, nil
}

func (s *concurrentReplaySource) Close() error {
	s.closed.Add(1)
	runtime.Gosched()
	return nil
}

func TestReplaySessionConcurrentViewsAndClose(t *testing.T) {
	caps, err := access.NewCapabilities(access.SequentialRead, access.StableSize)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := access.NewSnapshot("replay/test", access.StrongSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	source := &concurrentReplaySource{caps: caps, snapshot: snapshot}
	replayed := &replaySession{underlying: source, reader: source}

	start := make(chan struct{})
	ready := sync.WaitGroup{}
	var workers sync.WaitGroup
	operations := []func(){
		func() { _ = replayed.Capabilities() },
		func() { _, _ = replayed.Read(t.Context(), make([]byte, 1)) },
		func() { _, _ = replayed.Size(t.Context()) },
		func() { _, _ = replayed.Snapshot(t.Context()) },
	}
	ready.Add(len(operations))
	workers.Add(len(operations))
	for _, operation := range operations {
		operation := operation
		go func() {
			defer workers.Done()
			ready.Done()
			<-start
			for index := 0; index < 1000; index++ {
				operation()
				runtime.Gosched()
			}
		}()
	}
	ready.Wait()
	close(start)
	for index := 0; index < 100; index++ {
		runtime.Gosched()
	}
	if err := replayed.Close(); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	if source.closed.Load() != 1 {
		t.Fatalf("underlying Close calls = %d, want 1", source.closed.Load())
	}
	if source.calls.Load() == 0 {
		t.Fatal("concurrent replay operations did not reach the source")
	}
}

type blockingReplaySource struct {
	entered  chan struct{}
	release  chan struct{}
	caps     access.Capabilities
	closeCnt atomic.Int32
}

func (s *blockingReplaySource) Capabilities() access.Capabilities {
	close(s.entered)
	<-s.release
	return s.caps
}

func (*blockingReplaySource) Read(context.Context, []byte) (int, error) { return 0, io.EOF }

func (s *blockingReplaySource) Close() error {
	s.closeCnt.Add(1)
	return nil
}

func TestReplaySessionDoesNotHoldMutexDuringViewCallback(t *testing.T) {
	caps, err := access.NewCapabilities(access.SequentialRead)
	if err != nil {
		t.Fatal(err)
	}
	source := &blockingReplaySource{entered: make(chan struct{}), release: make(chan struct{}), caps: caps}
	replayed := &replaySession{underlying: source, reader: source}
	viewDone := make(chan struct{})
	go func() {
		_ = replayed.Capabilities()
		close(viewDone)
	}()
	<-source.entered
	closeDone := make(chan struct{})
	go func() {
		_ = replayed.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		close(source.release)
		<-viewDone
		t.Fatal("Close waited for a provider callback while Capabilities was in flight")
	}
	close(source.release)
	<-viewDone
	if source.closeCnt.Load() != 1 {
		t.Fatalf("underlying Close calls = %d, want 1", source.closeCnt.Load())
	}
}
