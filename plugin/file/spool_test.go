package file

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/plugin/pcm/linear"
	waveplugin "github.com/godexture/godec/plugin/wave"
	"github.com/godexture/godec/resource"
)

type sequentialOutputState struct {
	mu          sync.Mutex
	output      []byte
	selected    []access.Capability
	writeErr    error
	commitErr   error
	aborted     int
	closed      int
	partialSize int
}

type sequentialOutputSession struct {
	state     *sequentialOutputState
	staged    []byte
	committed bool
}

func (*sequentialOutputSession) Capabilities() access.Capabilities {
	value, _ := access.NewCapabilities(access.SequentialWrite)
	return value
}

func (s *sequentialOutputSession) Write(_ context.Context, source []byte) (int, error) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.state.writeErr != nil {
		return 0, s.state.writeErr
	}
	size := len(source)
	if s.state.partialSize > 0 {
		size = min(size, s.state.partialSize)
	}
	s.staged = append(s.staged, source[:size]...)
	return size, nil
}

func (*sequentialOutputSession) Flush(context.Context) error         { return nil }
func (*sequentialOutputSession) Sync(context.Context) error          { return nil }
func (*sequentialOutputSession) PrepareCommit(context.Context) error { return nil }

func (s *sequentialOutputSession) Commit(context.Context) error {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if s.state.commitErr != nil {
		return s.state.commitErr
	}
	s.state.output = append([]byte(nil), s.staged...)
	s.committed = true
	return nil
}

func (s *sequentialOutputSession) Abort(context.Context) error {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	if !s.committed {
		s.staged = nil
		s.state.aborted++
	}
	return nil
}

func (s *sequentialOutputSession) Close() error {
	s.state.mu.Lock()
	s.state.closed++
	s.staged = nil
	s.state.mu.Unlock()
	return nil
}

func TestWAVEOutputSpoolsToSequentialSinkWithExplicitPlanProjection(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.pcm")
	directPath := filepath.Join(directory, "direct.wav")
	payload := []byte{
		0x01, 0x00, 0xff, 0x7f,
		0xff, 0xff, 0x00, 0x80,
		0x34, 0x12, 0xcc, 0xed,
		0x00, 0x00, 0x01, 0x00,
	}
	if err := os.WriteFile(inputPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	policy := spoolPolicy(t, access.MemorySpool, 1<<20)
	directHost, err := host.New(host.Plugins(linear.Set().Add(Plugin()).Add(waveplugin.Plugin())))
	if err != nil {
		t.Fatal(err)
	}
	directRequest := waveOutputJob(t, inputPath, fileReference(t, directPath), policy)
	directPlan, err := directHost.Plan(t.Context(), directRequest)
	if err != nil {
		t.Fatal(err)
	}
	if outputBoundary(t, directPlan).Spool.Valid() {
		t.Fatal("direct random-write file output unexpectedly selected a spool")
	}
	result, err := directHost.Run(t.Context(), directRequest)
	if err != nil || !result.Succeeded() {
		t.Fatalf("direct WAVE result = %#v, %v", result, err)
	}
	direct, err := os.ReadFile(directPath)
	if err != nil {
		t.Fatal(err)
	}

	for _, storage := range []access.SpoolStorage{access.MemorySpool, access.DiskSpool} {
		t.Run(spoolStorageLabel(storage), func(t *testing.T) {
			state := &sequentialOutputState{output: []byte("old"), partialSize: 7}
			instance := sequentialOutputHost(t, state)
			request := waveOutputJob(t, inputPath, sequenceReference(t), spoolPolicy(t, storage, 1<<20))
			prepared, err := instance.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			boundary := outputBoundary(t, prepared.Plan())
			if len(boundary.Available) != 1 || boundary.Available[0] != access.SequentialWrite ||
				len(boundary.Effective) != 2 || boundary.Effective[0] != access.RandomWrite || boundary.Effective[1] != access.SequentialWrite ||
				len(boundary.Selected) != 1 || boundary.Selected[0] != access.RandomWrite || !boundary.Spool.Valid() || boundary.Spool.Storage() != storage || !boundary.Spool.FinalCopy() {
				t.Fatalf("spooled boundary = %#v", boundary)
			}
			result, err := prepared.Run(t.Context())
			if err != nil || !result.Succeeded() {
				t.Fatalf("spooled WAVE result = %#v, %v", result, err)
			}
			state.mu.Lock()
			output := append([]byte(nil), state.output...)
			selected := append([]access.Capability(nil), state.selected...)
			closed := state.closed
			state.mu.Unlock()
			if !bytes.Equal(output, direct) {
				t.Fatal("spooled WAVE differs from direct random-write output")
			}
			if len(selected) != 1 || selected[0] != access.SequentialWrite || closed != 1 {
				t.Fatalf("underlying selection/close = %v/%d", selected, closed)
			}
		})
	}
}

func TestWAVESpoolFailuresDoNotPublishSequentialTarget(t *testing.T) {
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input.pcm")
	if err := os.WriteFile(inputPath, []byte{1, 0, 2, 0, 3, 0, 4, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		maximum   resource.Bytes
		configure func(*sequentialOutputState)
		cancel    bool
	}{
		{name: "quota", maximum: 64},
		{name: "final copy", maximum: 1 << 20, configure: func(state *sequentialOutputState) { state.writeErr = errors.New("copy failed") }},
		{name: "commit", maximum: 1 << 20, configure: func(state *sequentialOutputState) { state.commitErr = errors.New("commit failed") }},
		{name: "cancel", maximum: 1 << 20, cancel: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := &sequentialOutputState{output: []byte("old")}
			if test.configure != nil {
				test.configure(state)
			}
			instance := sequentialOutputHost(t, state)
			request := waveOutputJob(t, inputPath, sequenceReference(t), spoolPolicy(t, access.MemorySpool, test.maximum))
			prepared, err := instance.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if test.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			result, err := prepared.Run(ctx)
			if err == nil || result.Succeeded() {
				t.Fatalf("failing spool result = %#v, %v", result, err)
			}
			state.mu.Lock()
			output := append([]byte(nil), state.output...)
			closed := state.closed
			state.mu.Unlock()
			if string(output) != "old" || closed != 1 {
				t.Fatalf("failed spool published/retained target = %q, closed %d", output, closed)
			}
		})
	}
}

func sequentialOutputHost(t *testing.T, state *sequentialOutputState) *host.Host {
	t.Helper()
	capabilities, err := access.NewCapabilities(access.SequentialWrite)
	if err != nil {
		t.Fatal(err)
	}
	acquire := func(_ context.Context, _ access.Reference, selection access.Selection) (access.Session, error) {
		state.mu.Lock()
		state.selected = selection.Capabilities()
		state.mu.Unlock()
		return &sequentialOutputSession{state: state}, nil
	}
	sink := sinkComponentWith(
		plugin.Descriptor{DisplayName: "Sequential fixture sink"},
		access.Sink("sequence", capabilities, access.AtomicReplace, acquire),
	)
	set := linear.Set().Add(Plugin()).Add(waveplugin.Plugin()).Override(SinkIdentity(), sink)
	instance, err := host.New(host.Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

func waveOutputJob(t *testing.T, inputPath string, outputReference access.Reference, policy job.Policy) job.Job {
	t.Helper()
	patch := config.NewPatch().
		SetText("rate", strconv.Itoa(48_000)).
		SetText("validBits", "16").
		SetText("layout", "stereo").
		SetText("endian", "little").
		SetText("chunkSamples", "2")
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("reader", linear.ReaderIdentity(), patch),
			job.NewNode("parser", linear.ParserIdentity(), patch),
			job.NewNode("mux", waveplugin.MuxerIdentity(), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("reader", "chunks"), job.At("parser", "chunks")),
			job.Connect(job.At("parser", "packets"), job.At("mux", "packets")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	input, err := job.InputFromReference(fileReference(t, inputPath))
	if err != nil {
		t.Fatal(err)
	}
	output, err := job.OutputToReference(outputReference)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph, job.WithPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func spoolPolicy(t *testing.T, storage access.SpoolStorage, maximum resource.Bytes) job.Policy {
	t.Helper()
	policy, ok := job.PolicyFor(job.Fast)
	if !ok {
		t.Fatal("Fast policy is unavailable")
	}
	policy.Resources.AllowSpool = true
	policy.Resources.SpoolMaxBytes = maximum
	policy.Resources.SpoolStorage = storage
	if !policy.Valid() {
		t.Fatalf("spool policy = %#v", policy.Resources)
	}
	return policy
}

func sequenceReference(t *testing.T) access.Reference {
	t.Helper()
	reference, err := access.Parse("sequence:output")
	if err != nil {
		t.Fatal(err)
	}
	return reference
}

func outputBoundary(t *testing.T, value plan.Plan) plan.Boundary {
	t.Helper()
	for _, boundary := range value.Boundaries() {
		if boundary.Direction == plan.OutputBoundary {
			return boundary
		}
	}
	t.Fatal("Plan has no output boundary")
	return plan.Boundary{}
}

func spoolStorageLabel(value access.SpoolStorage) string {
	if value == access.MemorySpool {
		return "memory"
	}
	return "disk"
}
