package conversion

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/godexture/core/pipeline"
)

type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Job runs a negotiated conversion in the background and tracks its outcome
// so progress can be polled independently of the goroutine driving Run. It
// is the shared state machine behind both the HTTP job store (example/web)
// and the WASM bindings: only how input/output are wired up and how job IDs
// are exposed differs between them.
type Job struct {
	pipeline *pipeline.Pipeline
	cancel   context.CancelFunc
	done     chan struct{}

	mu     sync.Mutex
	status Status
	err    error
}

// StartJob negotiates, builds, and runs a conversion in the background. The
// returned Job stays valid until Close is called; Snapshot and Cancel are
// safe to call concurrently with the running conversion.
func StartJob(ctx context.Context, inputs InputSet, output io.Writer, spec Spec) (*Job, error) {
	runCtx, cancel := context.WithCancel(ctx)
	// ObservationMetrics (not ObservationProgress) is required for per-node
	// state ("running"/"completed"/"failed"); Progress.Nodes is core to the
	// UI's node-by-node status display.
	built, err := Build(runCtx, inputs, output, spec, pipeline.ObservationMetrics)
	if err != nil {
		cancel()
		return nil, err
	}
	job := &Job{pipeline: built, cancel: cancel, done: make(chan struct{}), status: StatusRunning}
	go job.run(runCtx)
	return job, nil
}

func (j *Job) run(ctx context.Context) {
	err := j.pipeline.Run(ctx)
	status := StatusCompleted
	switch {
	case errors.Is(err, context.Canceled):
		status = StatusCanceled
	case err != nil:
		status = StatusFailed
	}
	j.mu.Lock()
	j.status, j.err = status, err
	j.mu.Unlock()
	close(j.done)
}

// Cancel requests that the job stop. It does not wait for the pipeline to
// finish; use Done or Close for that.
func (j *Job) Cancel() { j.cancel() }

// Done is closed once the job's goroutine has finished, after which
// Snapshot and Err reflect the final outcome.
func (j *Job) Done() <-chan struct{} { return j.done }

func (j *Job) Description() pipeline.Description { return j.pipeline.Description() }

// Snapshot reports the job's current progress and outcome.
func (j *Job) Snapshot() Progress {
	j.mu.Lock()
	status, err := j.status, j.err
	j.mu.Unlock()

	progress := Snapshot(j.pipeline.Snapshot(), status != StatusRunning)
	progress.Status = status
	if err != nil {
		progress.Error = err.Error()
	}
	return progress
}

// Err returns the error Run finished with, if any. Only meaningful after
// Done is closed.
func (j *Job) Err() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.err
}

// Close cancels the job if still running, waits for it to finish, and
// releases pipeline resources. Safe to call multiple times.
func (j *Job) Close() error {
	j.cancel()
	<-j.done
	return j.pipeline.Close()
}
