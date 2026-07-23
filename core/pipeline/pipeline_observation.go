package pipeline

import (
	"time"
)

func (p *Pipeline) Description() Description {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.description.Clone()
}

func (p *Pipeline) Snapshot() Snapshot {
	now := time.Now()
	p.mu.Lock()
	state := p.state.String()
	started := p.startedAt
	finished := p.finishedAt
	edges := append([]*edgeMetrics(nil), p.edgeMetrics...)
	nodes := append([]*nodeMetrics(nil), p.nodeMetrics...)
	description := p.description.Clone()
	p.mu.Unlock()

	end := finished
	if end.IsZero() && !started.IsZero() {
		end = now
	}
	var elapsed time.Duration
	if !started.IsZero() {
		elapsed = end.Sub(started)
	}
	snapshot := Snapshot{
		State:      state,
		StartedAt:  started,
		FinishedAt: finished,
		Elapsed:    elapsed,
		Nodes:      make([]NodeSnapshot, len(description.Nodes)),
		Edges:      make([]EdgeSnapshot, len(description.Edges)),
	}
	for i, current := range description.Nodes {
		snapshot.Nodes[i] = NodeSnapshot{Description: current, State: "unobserved"}
		if i < len(nodes) && nodes[i] != nil {
			snapshot.Nodes[i] = nodes[i].snapshot(now)
		}
	}
	for i, current := range description.Edges {
		snapshot.Edges[i] = EdgeSnapshot{Description: current}
		if i < len(edges) && edges[i] != nil {
			snapshot.Edges[i] = edges[i].snapshot()
		}
	}
	return snapshot
}
