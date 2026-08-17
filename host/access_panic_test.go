package host

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"
)

type panicSnapshotSession struct {
	caps      access.Capabilities
	panicAt   int32
	snapshots atomic.Int32
	closed    atomic.Int32
}

func (s *panicSnapshotSession) Capabilities() access.Capabilities { return s.caps }

func (s *panicSnapshotSession) Snapshot(context.Context) (access.Snapshot, error) {
	if s.snapshots.Add(1) == s.panicAt {
		panic("snapshot callback panic")
	}
	return access.NewSnapshot("panic-fixture", access.StrongSnapshot)
}

func (s *panicSnapshotSession) Read(context.Context, []byte) (int, error) { return 0, io.EOF }

func (s *panicSnapshotSession) ReadAt(context.Context, []byte, int64) (int, error) {
	return 0, io.EOF
}

func (s *panicSnapshotSession) Close() error {
	s.closed.Add(1)
	return nil
}

func TestAccessSnapshotPanicIsRecoveredAtEachRunBoundary(t *testing.T) {
	for _, test := range []struct {
		name  string
		at    int32
		phase Phase
	}{
		{name: "before-run", at: 2, phase: RunPhase},
		{name: "before-commit", at: 3, phase: CommitPhase},
	} {
		t.Run(test.name, func(t *testing.T) {
			session, sink, instance, request := panicSnapshotFixture(t, test.at)
			prepared, err := instance.Prepare(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			result, runErr := prepared.Run(context.Background())
			if runErr == nil {
				t.Fatal("Run succeeded after Snapshot panic")
			}
			failure := result.Primary
			if failure == nil {
				t.Fatalf("Run result = %#v, want structured Failure", result)
			}
			if failure.Phase != test.phase || failure.Node != "input-0" || failure.Task != "access/snapshot" {
				t.Fatalf("Failure = %#v, want phase %s input-0 access/snapshot", failure, test.phase)
			}
			if len(result.Outputs) != 1 || result.Outputs[0].State != OutputAborted {
				t.Fatalf("outputs = %#v, want one aborted output", result.Outputs)
			}
			if closeErr := prepared.Close(); closeErr != nil && !errors.Is(closeErr, runErr) {
				t.Fatalf("Prepared.Close = %v", closeErr)
			}
			if session.closed.Load() != 1 || sink.closed.Load() != 1 {
				t.Fatalf("session closes = source %d sink %d, want one each", session.closed.Load(), sink.closed.Load())
			}
			if snapshot := prepared.manager.Snapshot(); snapshot.Used.Memory != 0 || snapshot.Used.Workers != 0 || snapshot.Used.Queue != 0 || len(snapshot.Active) != 0 {
				t.Fatalf("resource manager retained %#v", snapshot)
			}
		})
	}
}

func TestInitialAccessSnapshotPanicClosesInputWithoutOutputAcquire(t *testing.T) {
	session, sink, instance, request := panicSnapshotFixture(t, 1)
	_, err := instance.Prepare(context.Background(), request)
	if err == nil {
		t.Fatal("Prepare succeeded after initial Snapshot panic")
	}
	if strings.Contains(err.Error(), "snapshot callback panic") {
		t.Fatalf("raw panic value escaped Prepare error: %v", err)
	}
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("Plan error = %v, want structured Failure", err)
	}
	if failure.Phase != PreparePhase || failure.Node != "input-0" || failure.Task != "access/snapshot" {
		t.Fatalf("Failure = %#v, want prepare/input-0/access/snapshot", failure)
	}
	if session.closed.Load() != 1 {
		t.Fatalf("input session closes = %d, want one", session.closed.Load())
	}
	if sink.acquired.Load() != 0 || sink.closed.Load() != 0 {
		t.Fatalf("output session acquired/closed = %d/%d, want 0/0", sink.acquired.Load(), sink.closed.Load())
	}
}

func TestProbeReadPanicDiscardsLease(t *testing.T) {
	for _, positioned := range []bool{false, true} {
		name := "sequential"
		capability := access.SequentialRead
		if positioned {
			name = "random"
			capability = access.RandomRead
		}
		t.Run(name, func(t *testing.T) {
			caps := mustCapabilities(t, capability)
			session := &panicReadSession{caps: caps, positioned: positioned}
			selection, ok := access.Select(caps, access.NewRequirements(access.AllOf(capability)))
			if !ok {
				t.Fatal("read capability was not selectable")
			}
			opening, err := access.NewOpening(access.SourceDirection, session, selection, 0)
			if err != nil {
				t.Fatal(err)
			}
			store, err := newProbeStore(opening, job.DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			store.node = "source"
			request, _ := access.NewRangeRequest(0, 1)
			_, _, err = store.Extend(context.Background(), []access.RangeRequest{request}, job.DefaultBudget().ProbeBytes)
			if err == nil {
				t.Fatal("probe succeeded after read panic")
			}
			var failure *Failure
			if !errors.As(err, &failure) {
				t.Fatalf("probe error = %v, want structured Failure", err)
			}
			if failure.Phase != PreparePhase || failure.Node != "source" || failure.Task == "" {
				t.Fatalf("Failure = %#v, want prepare/source/read task", failure)
			}
			if used := store.allocator.Used(); used != 0 {
				t.Fatalf("probe allocator retained %d bytes after panic", used)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type panicReadSession struct {
	caps       access.Capabilities
	positioned bool
}

func (s *panicReadSession) Capabilities() access.Capabilities { return s.caps }
func (*panicReadSession) Close() error                        { return nil }
func (s *panicReadSession) Read(context.Context, []byte) (int, error) {
	if s.positioned {
		return 0, io.EOF
	}
	panic("sequential read callback panic")
}
func (s *panicReadSession) ReadAt(context.Context, []byte, int64) (int, error) {
	if !s.positioned {
		return 0, io.EOF
	}
	panic("random read callback panic")
}

func panicSnapshotFixture(t *testing.T, panicAt int32) (*panicSnapshotSession, *sessionCounters, *Host, job.Job) {
	t.Helper()
	sourceCaps := mustCapabilities(t, access.SequentialRead)
	sinkCaps := mustCapabilities(t, access.SequentialWrite)
	session := &panicSnapshotSession{caps: sourceCaps, panicAt: panicAt}
	sinkState := &sessionCounters{}
	sourceAcquire := func(context.Context, access.Reference, access.Selection) (access.Session, error) {
		return session, nil
	}
	source, transform, sink, _ := boundaryComponentsWithRequirements(
		nil,
		[]plugin.ComponentOption{access.Source("memory", sourceCaps, sourceAcquire)},
		[]plugin.ComponentOption{access.Sink("memory", sinkCaps, access.AtomicReplace, sinkState.acquire(sinkCaps))},
		access.NewRequirements(access.AllOf(access.SequentialRead)),
	)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "panic snapshot", Version: "1"}, source, transform, sink))
	instance, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	inputReference, _ := access.Parse("memory:input")
	outputReference, _ := access.Parse("memory:output")
	input, _ := job.InputFromReference(inputReference)
	output, _ := job.OutputToReference(outputReference)
	graph, err := boundaryGraph(transform)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph)
	if err != nil {
		t.Fatal(err)
	}
	return session, sinkState, instance, request
}
