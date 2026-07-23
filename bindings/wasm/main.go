package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/godexture/sdk/catalog"
	"github.com/godexture/sdk/conversion"

	_ "github.com/godexture/codec-flac"
	_ "github.com/godexture/codec-mp3"
	_ "github.com/godexture/codec-pcm"
	_ "github.com/godexture/filter-audio"
	_ "github.com/godexture/format-flac"
	_ "github.com/godexture/format-mp3"
	_ "github.com/godexture/format-wav"
)

// Catalog returns the available demuxers, decoders, filters, encoders, and
// muxers, and which codecs each output format supports, as JSON.
func Catalog() (string, error) {
	return marshal(catalog.Build())
}

// Resolve negotiates a pipeline for input against the JSON-encoded spec
// without building or running it, returning the resolved node/edge topology
// as JSON. Used to preview a pipeline before starting a conversion.
func Resolve(input []byte, specJSON string) (string, error) {
	spec, err := unmarshalSpec(specJSON)
	if err != nil {
		return "", err
	}
	geometry, err := conversion.Negotiate(context.Background(), bytes.NewReader(input), io.Discard, spec)
	if err != nil {
		return "", err
	}
	defer geometry.Close()
	return marshal(geometry.Description())
}

// Start negotiates, builds, and begins a conversion in the background,
// returning a job ID. Poll Snapshot for progress and call Result once the
// job has finished.
func Start(input []byte, specJSON string) (string, error) {
	spec, err := unmarshalSpec(specJSON)
	if err != nil {
		return "", err
	}
	output := &bytes.Buffer{}
	job, err := conversion.StartJob(context.Background(), bytes.NewReader(input), output, spec)
	if err != nil {
		return "", err
	}
	return jobs.add(&jobEntry{job: job, output: output}), nil
}

// Snapshot reports a job's current progress and outcome as JSON.
func Snapshot(jobID string) (string, error) {
	entry, err := jobs.get(jobID)
	if err != nil {
		return "", err
	}
	return marshal(entry.job.Snapshot())
}

// Cancel requests that a running job stop.
func Cancel(jobID string) error {
	entry, err := jobs.get(jobID)
	if err != nil {
		return err
	}
	entry.job.Cancel()
	return nil
}

// Result waits for a job to finish and returns its output bytes, releasing
// the job's resources. Call it at most once per job.
func Result(jobID string) ([]byte, error) {
	entry, err := jobs.get(jobID)
	if err != nil {
		return nil, err
	}
	<-entry.job.Done()
	defer jobs.remove(jobID)
	defer entry.job.Close()
	if err := entry.job.Err(); err != nil {
		return nil, err
	}
	return entry.output.Bytes(), nil
}

var jobs = &jobStore{byID: make(map[string]*jobEntry)}

type jobEntry struct {
	job    *conversion.Job
	output *bytes.Buffer
}

type jobStore struct {
	mu   sync.Mutex
	byID map[string]*jobEntry
}

func (s *jobStore) add(entry *jobEntry) string {
	id := newJobID()
	s.mu.Lock()
	s.byID[id] = entry
	s.mu.Unlock()
	return id
}

func (s *jobStore) get(id string) (*jobEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.byID[id]
	if !ok {
		return nil, fmt.Errorf("unknown job %q", id)
	}
	return entry, nil
}

func (s *jobStore) remove(id string) {
	s.mu.Lock()
	delete(s.byID, id)
	s.mu.Unlock()
}

func newJobID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func marshal(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalSpec(specJSON string) (conversion.Spec, error) {
	var spec conversion.Spec
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return conversion.Spec{}, fmt.Errorf("invalid spec: %w", err)
	}
	return spec, nil
}

func main() {
	select {}
}
