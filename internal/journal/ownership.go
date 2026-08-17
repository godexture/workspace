package journal

import (
	"fmt"
	"sort"
	"sync"
)

// OwnershipError reports slot ownership left inconsistent after the run has
// joined and completed cleanup. Counts describe slots only and never retain a
// payload or its representation.
type OwnershipError struct {
	Live        int64
	Overrelease uint64
}

func (e *OwnershipError) Error() string {
	if e == nil {
		return "flow ownership audit failed"
	}
	return fmt.Sprintf("flow ownership audit failed: live slots %d, over-releases %d", e.Live, e.Overrelease)
}

type ownershipState struct {
	live        int64
	overrelease uint64
}

type ownershipTracker struct {
	mu     sync.Mutex
	nodes  map[string]ownershipState
	sealed bool
}

func newOwnershipTracker() *ownershipTracker {
	return &ownershipTracker{nodes: make(map[string]ownershipState)}
}

func (t *ownershipTracker) add(node string, delta int64) {
	if t == nil || delta != 1 && delta != -1 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sealed {
		return
	}
	state := t.nodes[node]
	state.live += delta
	if delta < 0 && state.live < 0 && state.overrelease != ^uint64(0) {
		state.overrelease++
	}
	t.nodes[node] = state
}

type ownershipImbalance struct {
	node        string
	live        int64
	overrelease uint64
}

func (t *ownershipTracker) seal() []ownershipImbalance {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sealed {
		return nil
	}
	t.sealed = true
	result := make([]ownershipImbalance, 0, len(t.nodes))
	for node, state := range t.nodes {
		if state.live == 0 && state.overrelease == 0 {
			continue
		}
		result = append(result, ownershipImbalance{node: node, live: state.live, overrelease: state.overrelease})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].node < result[right].node })
	return result
}

// EnableOwnershipAudit opts this run into slot accounting. Host calls it
// before it publishes any Domain or starts any task, so the pointer is
// immutable during execution and the ordinary disabled path needs neither an
// atomic nor an allocation.
func (l *Ledger) EnableOwnershipAudit() {
	if l != nil && l.ownership == nil {
		l.ownership = newOwnershipTracker()
	}
}

// RecordOwnershipFailures seals the audit after all cleanup and records each
// bad node once as a Resource cleanup failure. Calling it again is a no-op.
func (l *Ledger) RecordOwnershipFailures() {
	if l == nil || l.ownership == nil {
		return
	}
	for _, imbalance := range l.ownership.seal() {
		l.Record(Entry{
			Kind:      CleanupError,
			Operation: Resource,
			Task:      "runtime/ownership",
			Node:      imbalance.node,
			Err: &OwnershipError{
				Live:        imbalance.live,
				Overrelease: imbalance.overrelease,
			},
		})
	}
}
