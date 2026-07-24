package pipeline

import (
	"time"
)

type ObservationMode uint8

const (
	ObservationOff ObservationMode = iota
	ObservationProgress
	ObservationMetrics
)

type NodeSnapshot struct {
	Description NodeDescription
	State       string
	StartedAt   time.Time
	FinishedAt  time.Time
	Elapsed     time.Duration
	Error       string
}

type EdgeSnapshot struct {
	Description EdgeDescription
	Items       uint64
	Bytes       uint64
	Samples     uint64
	MediaTime   time.Duration
}

type Snapshot struct {
	State      string
	StartedAt  time.Time
	FinishedAt time.Time
	Elapsed    time.Duration
	Nodes      []NodeSnapshot
	Edges      []EdgeSnapshot
}
