package pipeline

import (
	"sync"
	"sync/atomic"
	"time"
)

type edgeMetrics struct {
	description EdgeDescription
	items       atomic.Uint64
	bytes       atomic.Uint64
	samples     atomic.Uint64
	mediaNanos  atomic.Int64
}

func (m *edgeMetrics) snapshot() EdgeSnapshot {
	return EdgeSnapshot{
		Description: (Description{Edges: []EdgeDescription{m.description}}).Clone().Edges[0],
		Items:       m.items.Load(),
		Bytes:       m.bytes.Load(),
		Samples:     m.samples.Load(),
		MediaTime:   time.Duration(m.mediaNanos.Load()),
	}
}

func (m *edgeMetrics) updateMediaTime(value time.Duration) {
	nanos := int64(value)
	for current := m.mediaNanos.Load(); nanos > current; current = m.mediaNanos.Load() {
		if m.mediaNanos.CompareAndSwap(current, nanos) {
			return
		}
	}
}

type nodeMetrics struct {
	description NodeDescription
	mu          sync.Mutex
	state       string
	startedAt   time.Time
	finishedAt  time.Time
	err         string
}

func newNodeMetrics(description NodeDescription) *nodeMetrics {
	return &nodeMetrics{description: description, state: "ready"}
}

func (m *nodeMetrics) start() {
	m.mu.Lock()
	m.state = "running"
	m.startedAt = time.Now()
	m.mu.Unlock()
}

func (m *nodeMetrics) finish(err error) {
	m.mu.Lock()
	m.finishedAt = time.Now()
	if err != nil {
		m.state = "failed"
		m.err = err.Error()
	} else {
		m.state = "completed"
	}
	m.mu.Unlock()
}

func (m *nodeMetrics) snapshot(now time.Time) NodeSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	finished := m.finishedAt
	if finished.IsZero() && !m.startedAt.IsZero() {
		finished = now
	}
	var elapsed time.Duration
	if !m.startedAt.IsZero() {
		elapsed = finished.Sub(m.startedAt)
	}
	return NodeSnapshot{
		Description: (Description{Nodes: []NodeDescription{m.description}}).Clone().Nodes[0],
		State:       m.state,
		StartedAt:   m.startedAt,
		FinishedAt:  m.finishedAt,
		Elapsed:     elapsed,
		Error:       m.err,
	}
}
