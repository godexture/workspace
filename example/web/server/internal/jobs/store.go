// Package jobs owns the lifetime of Server-mode conversions: temp files on
// disk, and the background conversion.Job driving each one. A job survives
// independently of any single HTTP request (including the SSE stream that
// watches it) and is only stopped by an explicit Remove.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/sdk/conversion"
)

// Input identifies one file used by a conversion. Shared preset assets are
// unowned so removing a job never deletes application data.
type Input struct {
	Path  string
	Owned bool
}

type Inputs struct {
	Main Input
	Aux  map[string]Input
}

type Job struct {
	ID          string
	conv        *conversion.Job
	ownedInputs []string
	outputPath  string
	filesClosed chan struct{} // closed once every file handle is closed
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

// Start begins a conversion detached from its originating HTTP request. It
// owns every uploaded input while shared preset paths remain untouched.
func (s *Store) Start(inputs Inputs, spec conversion.Spec) (*Job, error) {
	main, err := os.Open(inputs.Main.Path)
	if err != nil {
		return nil, err
	}
	aux := make(map[string]io.ReadSeeker, len(inputs.Aux))
	openFiles := []io.Closer{main}
	for _, name := range slices.Sorted(maps.Keys(inputs.Aux)) {
		file, openErr := os.Open(inputs.Aux[name].Path)
		if openErr != nil {
			closeFiles(openFiles)
			return nil, openErr
		}
		aux[name] = file
		openFiles = append(openFiles, file)
	}

	id := newID()
	outputPath := filepath.Join(s.dir, "output-"+id)
	output, err := os.Create(outputPath)
	if err != nil {
		closeFiles(openFiles)
		return nil, err
	}

	conv, err := conversion.StartJob(context.Background(), conversion.InputSet{Main: main, Aux: aux}, output, spec)
	if err != nil {
		closeFiles(openFiles)
		_ = output.Close()
		_ = os.Remove(outputPath)
		return nil, err
	}

	job := &Job{
		ID:          id,
		conv:        conv,
		ownedInputs: ownedInputs(inputs),
		outputPath:  outputPath,
		filesClosed: make(chan struct{}),
	}
	go func() {
		<-conv.Done()
		closeFiles(openFiles)
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
// deletes its owned input/output files. Safe to call on an unknown ID.
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
	<-job.filesClosed
	for _, path := range job.ownedInputs {
		_ = os.Remove(path)
	}
	_ = os.Remove(job.outputPath)
}

func ownedInputs(inputs Inputs) []string {
	result := make([]string, 0, len(inputs.Aux)+1)
	if inputs.Main.Owned {
		result = append(result, inputs.Main.Path)
	}
	for _, input := range inputs.Aux {
		if input.Owned {
			result = append(result, input.Path)
		}
	}
	return result
}

func closeFiles(files []io.Closer) {
	for _, file := range files {
		_ = file.Close()
	}
}

func newID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
