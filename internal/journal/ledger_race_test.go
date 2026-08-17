package journal

import (
	"errors"
	"sync"
	"testing"
)

// Record must not return a pointer into Ledger.stopped.  A cleanup cause can
// be superseded by a later work failure while a caller is still propagating
// the first cause; returning the mutable storage would race that replacement.
func TestRecordEchoKeepsAnIndependentFailureCopy(t *testing.T) {
	ledger := NewLedger()
	first := ledger.Record(Entry{Kind: CleanupError, Operation: Close, Err: errors.New("cleanup")})
	if first == nil {
		t.Fatal("initial cleanup was not recorded")
	}
	echo := ledger.Record(Entry{Kind: WorkError, Operation: Run, Err: newCause(*first)})
	if echo == nil || echo.ID != first.ID {
		t.Fatalf("echo = %#v, want the cleanup event", echo)
	}

	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		for index := 0; index < 100_000; index++ {
			ledger.Record(Entry{Kind: WorkError, Operation: Run, Err: errors.New("work")})
		}
	}()
	for index := 0; index < 100_000; index++ {
		_ = echo.ID
		_ = echo.Kind
		_ = echo.Err
	}
	wait.Wait()
	if echo.ID != first.ID || echo.Kind != first.Kind {
		t.Fatalf("echo changed after stopping provenance moved: got %+v, want %+v", echo, first)
	}
}
