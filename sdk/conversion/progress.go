package conversion

import (
	"time"

	"github.com/godexture/godec/core/pipeline"
)

type NodeStatus struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	Plugin       string `json:"plugin"`
	AutoInserted bool   `json:"autoInserted"`
	State        string `json:"state"`
	ElapsedMs    int64  `json:"elapsedMs"`
	Error        string `json:"error,omitempty"`
}

type Progress struct {
	Status         Status       `json:"status,omitempty"` // set by Job.Snapshot; empty when Snapshot is called directly
	Error          string       `json:"error,omitempty"`  // set by Job.Snapshot when Status is failed
	Percent        float64      `json:"percent"`          // -1 when duration is unknown
	ProcessedMs    int64        `json:"processedMs"`
	TotalMs        int64        `json:"totalMs"`
	ProcessedItems uint64       `json:"processedItems"`
	SpeedRatio     float64      `json:"speedRatio"` // processed media time per wall-clock second
	ElapsedMs      int64        `json:"elapsedMs"`
	EtaMs          int64        `json:"etaMs"`
	Nodes          []NodeStatus `json:"nodes"`
}

// Snapshot summarizes a pipeline.Snapshot for progress reporting. Elapsed
// time is taken from snapshot.Elapsed (wall-clock time since Pipeline.Run
// started). Set final to true once the conversion has finished (successfully
// or not) so the reported percent/duration reflect completion rather than
// the last observed media time.
func Snapshot(snapshot pipeline.Snapshot, final bool) Progress {
	elapsed := snapshot.Elapsed
	progress := Progress{
		Percent:   -1,
		ElapsedMs: elapsed.Milliseconds(),
		Nodes:     make([]NodeStatus, len(snapshot.Nodes)),
	}
	for i, node := range snapshot.Nodes {
		progress.Nodes[i] = NodeStatus{
			ID:           node.Description.ID,
			Role:         string(node.Description.Role),
			Plugin:       node.Description.Plugin,
			AutoInserted: node.Description.AutoInserted,
			State:        node.State,
			ElapsedMs:    node.Elapsed.Milliseconds(),
			Error:        node.Error,
		}
	}

	var source *pipeline.EdgeSnapshot
	for i := range snapshot.Edges {
		if snapshot.Edges[i].Description.ProgressSource {
			source = &snapshot.Edges[i]
			break
		}
	}
	if source == nil {
		return progress
	}
	progress.ProcessedItems = source.Items

	duration := source.Description.Stream.Duration
	if duration <= 0 {
		return progress
	}
	processed := source.MediaTime
	if final {
		processed = duration
	}
	if processed > duration {
		processed = duration
	}
	progress.ProcessedMs = processed.Milliseconds()
	progress.TotalMs = duration.Milliseconds()
	progress.Percent = float64(processed) / float64(duration) * 100
	if elapsed > 0 {
		progress.SpeedRatio = float64(processed) / float64(elapsed)
	}
	if progress.SpeedRatio > 0 && processed < duration {
		progress.EtaMs = time.Duration(float64(duration-processed) / progress.SpeedRatio).Milliseconds()
	}
	return progress
}
