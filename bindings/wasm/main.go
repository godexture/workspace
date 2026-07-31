package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/godexture/godec/sdk/catalog"
	"github.com/godexture/godec/sdk/conversion"

	_ "github.com/godexture/godec/plugins/codec-flac"
	_ "github.com/godexture/godec/plugins/codec-mp3"
	_ "github.com/godexture/godec/plugins/codec-pcm"
	_ "github.com/godexture/godec/plugins/filter-audio"
	_ "github.com/godexture/godec/plugins/format-flac"
	_ "github.com/godexture/godec/plugins/format-mp3"
	_ "github.com/godexture/godec/plugins/format-wav"
)

// Catalog returns the available demuxers, decoders, filters, encoders, and
// muxers, and which codecs each output format supports, as JSON.
func Catalog() (string, error) {
	return marshal(catalog.Build())
}

// DescribeFilter resolves a filter's editable configuration and port topology
// for the given structural parameters.
func DescribeFilter(name string, parameters map[string]string) (string, error) {
	entry, err := catalog.DescribeFilter(name, parameters)
	if err != nil {
		return "", err
	}
	return marshal(entry)
}

// ResolveConfiguration applies defaults and normalization to plugin values
// and returns their effective values and dynamic field state.
func ResolveConfiguration(role, name string, parameters, values map[string]string) (string, error) {
	resolution, err := catalog.ResolveConfiguration(role, name, parameters, values)
	if err != nil {
		return "", err
	}
	return marshal(resolution)
}

// Resolve negotiates a pipeline for inputs against the JSON-encoded spec
// without building or running it, returning the resolved node/edge topology
// as JSON. Used to preview a pipeline before starting a conversion.
func Resolve(mainInput []byte, auxInputs map[string][]byte, specJSON string) (string, error) {
	spec, err := unmarshalSpec(specJSON)
	if err != nil {
		return "", err
	}
	geometry, err := conversion.Negotiate(context.Background(), inputSet(mainInput, auxInputs), io.Discard, spec)
	if err != nil {
		return "", err
	}
	defer geometry.Close()
	return marshal(geometry.Description())
}

// Start negotiates, builds, and begins a conversion in the background under
// jobID (chosen by the caller, since Start's own return value only reaches
// JS once the conversion is done -- see reportProgress). onProgress is
// invoked with a JSON-encoded Progress a few times a second until the job
// finishes, and once more with the final outcome.
func Start(jobID string, mainInput []byte, auxInputs map[string][]byte, specJSON string, onProgress func(string)) (string, error) {
	spec, err := unmarshalSpec(specJSON)
	if err != nil {
		return "", err
	}
	output := &bytes.Buffer{}
	job, err := conversion.StartJob(context.Background(), inputSet(mainInput, auxInputs), output, spec)
	if err != nil {
		return "", err
	}
	if err := jobs.add(jobID, &jobEntry{job: job, output: output}); err != nil {
		job.Close()
		return "", err
	}
	go reportProgress(job, onProgress)
	return jobID, nil
}

// reportProgress pushes progress snapshots to onProgress until job finishes.
// Goroutines on js/wasm are scheduled cooperatively with no real
// preemption, so a CPU-bound conversion never hands control back to the JS
// event loop on its own -- control only returns once every goroutine is
// blocked on something JS-mediated. A time.Sleep/time.Ticker here would
// never fire mid-conversion for the same reason (its timer callback can't
// run until JS regains control), so this instead spins on runtime.Gosched,
// which is a purely internal scheduler yield: it interleaves with the
// pipeline's own goroutines (which yield constantly at their channel
// hand-offs) without needing a JS round-trip.
func reportProgress(job *conversion.Job, onProgress func(string)) {
	const interval = 200 * time.Millisecond
	last := time.Now()
	for {
		select {
		case <-job.Done():
			pushSnapshot(job, onProgress)
			return
		default:
		}
		if time.Since(last) >= interval {
			pushSnapshot(job, onProgress)
			last = time.Now()
		}
		runtime.Gosched()
	}
}

func pushSnapshot(job *conversion.Job, onProgress func(string)) {
	if data, err := marshal(job.Snapshot()); err == nil {
		onProgress(data)
	}
}

func inputSet(mainInput []byte, auxInputs map[string][]byte) conversion.InputSet {
	aux := make(map[string]io.ReadSeeker, len(auxInputs))
	for name, input := range auxInputs {
		aux[name] = bytes.NewReader(input)
	}
	return conversion.InputSet{Main: bytes.NewReader(mainInput), Aux: aux}
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

func (s *jobStore) add(id string, entry *jobEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.byID[id]; exists {
		return fmt.Errorf("job %q already exists", id)
	}
	s.byID[id] = entry
	return nil
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
