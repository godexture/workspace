package parallel

import (
	"sync"
	"testing"
	"time"
)

// waitForParked polls until the gate has a live wait channel, i.e. some
// caller has reached the blocking receive in Wait. Used to avoid racing
// MarkReady against a waiter that hasn't parked yet.
func waitForParked(t *testing.T, g *Gate) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		g.Lock()
		parked := g.waitCh != nil
		g.Unlock()
		if parked {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for a waiter to park on the gate")
}

func TestGate_ChanLocked_SharesChannel(t *testing.T) {
	g := &Gate{}
	g.Lock()
	ch1 := g.ChanLocked()
	ch2 := g.ChanLocked()
	g.Unlock()

	if ch1 != ch2 {
		t.Fatal("ChanLocked created a new channel on the second call instead of reusing the pending one")
	}
}

func TestGate_Wait_ReadyBeforeWaiting(t *testing.T) {
	g := &Gate{}

	done := make(chan struct{})
	go func() {
		g.Wait(func() bool { return true })
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait blocked even though the readiness predicate was already true")
	}
}

func TestGate_Wait_BlockedWaiterWakesOnMarkReady(t *testing.T) {
	g := &Gate{}
	var ready bool

	done := make(chan struct{})
	go func() {
		g.Wait(func() bool { return ready })
		close(done)
	}()

	waitForParked(t, g)

	select {
	case <-done:
		t.Fatal("Wait returned before MarkReady was called")
	default:
	}

	g.MarkReady(func() { ready = true })

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not wake up after MarkReady")
	}
}

func TestGate_Wait_MultipleWaitersShareNotification(t *testing.T) {
	g := &Gate{}
	var ready bool

	const waiters = 8
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			g.Wait(func() bool { return ready })
		}()
	}

	waitForParked(t, g)

	g.MarkReady(func() { ready = true })

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("not all waiters woke up from a single MarkReady notification")
	}
}

func TestGate_Wait_RepeatedUnrelatedMarkReadyDoesNotLoseWakeup(t *testing.T) {
	g := &Gate{}
	var unrelatedCount int
	var ready bool

	done := make(chan struct{})
	go func() {
		g.Wait(func() bool { return ready })
		close(done)
	}()

	// Wake the gate a few times for state changes the waiter doesn't care
	// about, to make sure each spurious notification is followed by a
	// fresh channel rather than leaving the waiter permanently unparked.
	for i := 0; i < 3; i++ {
		waitForParked(t, g)
		select {
		case <-done:
			t.Fatalf("Wait returned early after %d unrelated MarkReady wakeups", i)
		default:
		}
		g.MarkReady(func() { unrelatedCount++ })
	}

	waitForParked(t, g)
	g.MarkReady(func() { ready = true })

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not wake up after the readiness predicate became true")
	}
	if unrelatedCount != 3 {
		t.Fatalf("expected 3 unrelated MarkReady calls to have run, got %d", unrelatedCount)
	}
}
