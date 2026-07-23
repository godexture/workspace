// Package jobs owns the lifetime of Server-mode conversions: temp files on
// disk, and the background conversion.Job driving each one. A job survives
// independently of any single HTTP request (including the SSE stream that
// watches it) and is only stopped by an explicit Remove.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"

	"github.com/godexture/core/pipeline"
	"github.com/godexture/sdk/conversion"
)

type Job struct {
	ID          string
	conv        *conversion.Job
	inputPath   string
	inputOwned  bool // false for preset inputs: shared assets, never deleted
	outputPath  string
	filesClosed chan struct{} // closed once the input/output *os.File handles are closed
}

func (j *Job) Snapshot() conversion.Progress     { return j.conv.Snapshot() }
func (j *Job) Cancel()                           { j.conv.Cancel() }
func (j *Job) Done() <-chan struct{}             { return j.conv.Done() }
func (j *Job) Err() error                        { return j.conv.Err() }
func (j *Job) OutputPath() string                { return j.outputPath }
func (j *Job) Description() pipeline.Description { return j.conv.Description() }

type Store struct {
	dir string

	mu   sync.Mutex
	byID map[string]*Job
}

// NewStore creates a Store that keeps job input/output files under dir,
// creating it if necessary.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir, byID: make(map[string]*Job)}, nil
}

// CreateInputFile returns a new file under the store's directory for the
// caller to write an upload into before calling Start. The caller owns the
// file until Start (which takes ownership) or the caller removes it itself
// on error.
func (s *Store) CreateInputFile() (*os.File, error) {
	return os.CreateTemp(s.dir, "input-*")
}

// Start begins a conversion reading from the file at inputPath and takes
// ownership of the job's lifetime, running detached from any HTTP request
// so it keeps going after the originating request completes; only
// Cancel/Remove stop it early. When owned is true, inputPath is deleted by
// Remove (e.g. an uploaded file copied via CreateInputFile); when false, it
// is treated as a shared, read-only asset (e.g. a preset) and never
// deleted.
func (s *Store) Start(inputPath string, owned bool, spec conversion.Spec) (*Job, error) {
	input, err := os.Open(inputPath)
	if err != nil {
		return nil, err
	}

	id := newID()
	outputPath := filepath.Join(s.dir, "output-"+id)
	output, err := os.Create(outputPath)
	if err != nil {
		_ = input.Close()
		return nil, err
	}

	conv, err := conversion.StartJob(context.Background(), conversion.InputSet{Main: input}, output, spec)
	if err != nil {
		_ = input.Close()
		_ = output.Close()
		_ = os.Remove(outputPath)
		return nil, err
	}

	job := &Job{
		ID: id, conv: conv, inputPath: inputPath, inputOwned: owned, outputPath: outputPath,
		filesClosed: make(chan struct{}),
	}
	go func() {
		<-conv.Done()
		_ = input.Close()
		_ = output.Close()
		close(job.filesClosed)
	}()

	s.mu.Lock()
	s.byID[id] = job
	s.mu.Unlock()
	return job, nil
}

func (s *Store) Get(id string) (*Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.byID[id]
	return job, ok
}

// Remove cancels the job if still running, waits for it to finish, and
// deletes its input/output files. Safe to call on an unknown ID (no-op).
func (s *Store) Remove(id string) {
	s.mu.Lock()
	job, ok := s.byID[id]
	if ok {
		delete(s.byID, id)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	_ = job.conv.Close()
	<-job.filesClosed // conv.Close() only guarantees Done() fired, not that the file-closing goroutine below has run yet
	if job.inputOwned {
		_ = os.Remove(job.inputPath)
	}
	_ = os.Remove(job.outputPath)
}

func newID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
