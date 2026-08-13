// Package parallel provides a completion-notification primitive for
// producer/consumer pipelines where jobs complete out of order on a worker
// pool: every pending entry that needs to block a waiter shares one
// lazily-created channel instead of owning its own, so signaling costs one
// channel per contended wait rather than one per job.
//
// It has no consumer in the current tree: the codecs that used it are still
// in _legacy pending the M8 family migration, which also decides whether this
// stays a public package. Treat the API as unstable until then.
package parallel

import "sync"

// Gate coordinates readiness notifications between worker-pool tasks and
// goroutines blocked waiting for output.
type Gate struct {
	mu     sync.Mutex
	waitCh chan struct{}
}

// MarkReady runs markEntryReady (typically "entry.ready = true") under the
// gate's lock, then wakes anyone currently blocked in Wait or on a channel
// from ChanLocked. Safe to call from a pool worker goroutine.
func (g *Gate) MarkReady(markEntryReady func()) {
	g.mu.Lock()
	markEntryReady()
	if g.waitCh != nil {
		close(g.waitCh)
		g.waitCh = nil
	}
	g.mu.Unlock()
}

// ChanLocked returns the channel that closes the next time MarkReady is
// called, creating it on first use. The gate's lock must be held (via Lock
// or from within Wait's isReady callback).
func (g *Gate) ChanLocked() <-chan struct{} {
	if g.waitCh == nil {
		g.waitCh = make(chan struct{})
	}
	return g.waitCh
}

// Wait blocks until isReady reports true, evaluating it under the gate's
// lock so it can safely read entry state that MarkReady mutates.
func (g *Gate) Wait(isReady func() bool) {
	for {
		g.mu.Lock()
		if isReady() {
			g.mu.Unlock()
			return
		}
		ch := g.ChanLocked()
		g.mu.Unlock()
		<-ch
	}
}

// Lock and Unlock let callers (e.g. OutputReady) guard entry/queue state
// with the same lock ChanLocked and Wait use.
func (g *Gate) Lock()   { g.mu.Lock() }
func (g *Gate) Unlock() { g.mu.Unlock() }
