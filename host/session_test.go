package host

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type sessionCounters struct {
	acquired    atomic.Int32
	closed      atomic.Int32
	fail        error
	closeErr    error
	actual      access.Capabilities
	noViews     bool
	noSize      bool
	noSnapshot  bool
	snapshotErr bool
	wait        bool
}

type trackedAccessSession struct {
	capabilities access.Capabilities
	counters     *sessionCounters
}

type capabilityOnlyAccessSession struct {
	capabilities access.Capabilities
	counters     *sessionCounters
}

type noSnapshotAccessSession struct {
	capabilities access.Capabilities
	counters     *sessionCounters
}

type noStableSizeAccessSession struct {
	capabilities access.Capabilities
	counters     *sessionCounters
}

type snapshotErrorAccessSession struct {
	capabilities access.Capabilities
	counters     *sessionCounters
}

func (s capabilityOnlyAccessSession) Capabilities() access.Capabilities { return s.capabilities }
func (s capabilityOnlyAccessSession) Close() error {
	s.counters.closed.Add(1)
	return s.counters.closeErr
}

func (s trackedAccessSession) Capabilities() access.Capabilities { return s.capabilities }
func (s trackedAccessSession) Close() error {
	s.counters.closed.Add(1)
	return s.counters.closeErr
}
func (s trackedAccessSession) Snapshot(context.Context) (access.Snapshot, error) {
	if s.counters.snapshotErr {
		return access.Snapshot{}, access.ErrNoSnapshot
	}
	return access.NewSnapshot("host/test/empty", access.StrongSnapshot)
}
func (trackedAccessSession) Read(context.Context, []byte) (int, error) { return 0, io.EOF }
func (trackedAccessSession) ReadAt(context.Context, []byte, int64) (int, error) {
	return 0, io.EOF
}
func (trackedAccessSession) Write(_ context.Context, value []byte) (int, error) {
	return len(value), nil
}
func (trackedAccessSession) Flush(context.Context) error         { return nil }
func (trackedAccessSession) Sync(context.Context) error          { return nil }
func (trackedAccessSession) PrepareCommit(context.Context) error { return nil }
func (trackedAccessSession) Commit(context.Context) error        { return nil }
func (trackedAccessSession) Abort(context.Context) error         { return nil }

func (c *sessionCounters) acquire(capabilities access.Capabilities) access.AcquireFunc {
	return func(ctx context.Context, _ access.Reference, _ access.Selection) (access.Session, error) {
		c.acquired.Add(1)
		if c.wait {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if c.fail != nil {
			return nil, c.fail
		}
		actual := capabilities
		if len(c.actual.Values()) != 0 {
			actual = c.actual
		}
		if c.noViews {
			return capabilityOnlyAccessSession{capabilities: actual, counters: c}, nil
		}
		if c.noSnapshot {
			return noSnapshotAccessSession{capabilities: actual, counters: c}, nil
		}
		if c.snapshotErr {
			return snapshotErrorAccessSession{capabilities: actual, counters: c}, nil
		}
		if c.noSize {
			return noStableSizeAccessSession{capabilities: actual, counters: c}, nil
		}
		return trackedAccessSession{capabilities: actual, counters: c}, nil
	}
}

func (s noSnapshotAccessSession) Capabilities() access.Capabilities { return s.capabilities }
func (s noSnapshotAccessSession) Close() error {
	s.counters.closed.Add(1)
	return s.counters.closeErr
}
func (noSnapshotAccessSession) Read(context.Context, []byte) (int, error) { return 0, io.EOF }
func (noSnapshotAccessSession) ReadAt(context.Context, []byte, int64) (int, error) {
	return 0, io.EOF
}
func (noSnapshotAccessSession) Size(context.Context) (int64, error) { return 0, nil }

func (s noStableSizeAccessSession) Capabilities() access.Capabilities { return s.capabilities }
func (s noStableSizeAccessSession) Close() error {
	s.counters.closed.Add(1)
	return s.counters.closeErr
}
func (noStableSizeAccessSession) Read(context.Context, []byte) (int, error) { return 0, io.EOF }
func (noStableSizeAccessSession) ReadAt(context.Context, []byte, int64) (int, error) {
	return 0, io.EOF
}

func (s snapshotErrorAccessSession) Capabilities() access.Capabilities { return s.capabilities }
func (s snapshotErrorAccessSession) Close() error {
	s.counters.closed.Add(1)
	return s.counters.closeErr
}
func (snapshotErrorAccessSession) Read(context.Context, []byte) (int, error) { return 0, io.EOF }
func (snapshotErrorAccessSession) ReadAt(context.Context, []byte, int64) (int, error) {
	return 0, io.EOF
}
func (snapshotErrorAccessSession) Size(context.Context) (int64, error) { return 0, nil }
func (snapshotErrorAccessSession) Snapshot(context.Context) (access.Snapshot, error) {
	return access.Snapshot{}, access.ErrNoSnapshot
}

func TestPlanningDurationStartsBeforeInputAcquire(t *testing.T) {
	source, sink, instance, request := providerSessionFixture(t)
	source.wait = true
	graph, _ := request.Graph()
	budget := request.Budget()
	budget.Duration = time.Millisecond
	timed, err := job.New(request.Inputs(), request.Outputs(), graph, job.WithPolicy(request.Policy()), job.WithBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	_, err = instance.Plan(t.Context(), timed)
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "prepare.budget-exhausted" || items[0].Detail["dimension"] != "duration" || items[0].Detail["phase"] != "acquire" || items[0].Detail["limit"] != budget.Duration.String() {
		t.Fatalf("duration diagnostic = %#v, error=%v", items, err)
	}
	if source.acquired.Load() != 1 || source.closed.Load() != 0 || sink.acquired.Load() != 0 {
		t.Fatalf("duration cleanup = source %d/%d, sink %d", source.acquired.Load(), source.closed.Load(), sink.acquired.Load())
	}
}

func TestPlanAcquiresAndClosesOnlyInputSession(t *testing.T) {
	source, sink, instance, request := providerSessionFixture(t)
	if _, err := instance.Plan(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if source.acquired.Load() != 1 || source.closed.Load() != 1 {
		t.Fatalf("source sessions = acquired %d, closed %d", source.acquired.Load(), source.closed.Load())
	}
	if sink.acquired.Load() != 0 || sink.closed.Load() != 0 {
		t.Fatalf("dry-run opened output session = acquired %d, closed %d", sink.acquired.Load(), sink.closed.Load())
	}
}

func TestPreparedOwnsProviderSessionsUntilClose(t *testing.T) {
	source, sink, instance, request := providerSessionFixture(t)
	prepared, err := instance.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if source.acquired.Load() != 1 || sink.acquired.Load() != 1 || source.closed.Load() != 0 || sink.closed.Load() != 0 {
		t.Fatalf("prepared sessions = source %d/%d, sink %d/%d", source.acquired.Load(), source.closed.Load(), sink.acquired.Load(), sink.closed.Load())
	}
	boundaries := prepared.Plan().Boundaries()
	if len(boundaries) != 2 {
		t.Fatalf("boundaries = %#v", boundaries)
	}
	inputOpening := prepared.bySession[boundaries[0].Node].opening
	outputOpening := prepared.bySession[boundaries[1].Node].opening
	if _, ok := access.SequentialOf(inputOpening); !ok {
		t.Fatal("Prepared input has no selected Sequential view")
	}
	if _, ok := access.RandomOf(inputOpening); ok {
		t.Fatal("Prepared input exposes unselected Random view")
	}
	if _, ok := access.AppenderOf(outputOpening); !ok {
		t.Fatal("Prepared output has no selected Appender view")
	}
	if _, ok := access.PatcherOf(outputOpening); ok {
		t.Fatal("Prepared output exposes unselected Patcher view")
	}
	if _, ok := access.TransactionOf(outputOpening); !ok {
		t.Fatal("Prepared output has no transaction view")
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if source.closed.Load() != 1 || sink.closed.Load() != 1 {
		t.Fatalf("session close counts = source %d, sink %d", source.closed.Load(), sink.closed.Load())
	}
}

func TestPreparedRunClosesProviderSessions(t *testing.T) {
	source, sink, instance, request := providerSessionFixture(t)
	result, err := instance.Run(context.Background(), request)
	if err != nil || !result.Succeeded() {
		t.Fatalf("Run result = %#v, error %v", result, err)
	}
	if source.closed.Load() != 1 || sink.closed.Load() != 1 {
		t.Fatalf("Run session close counts = source %d, sink %d", source.closed.Load(), sink.closed.Load())
	}
}

func TestPrepareClosesAcquiredSessionsAfterLaterAcquireFailure(t *testing.T) {
	want := errors.New("sink acquire failed")
	source, sink, instance, request := providerSessionFixture(t)
	sink.fail = want
	if _, err := instance.Prepare(context.Background(), request); !errors.Is(err, want) {
		t.Fatalf("Prepare error = %v", err)
	}
	if source.closed.Load() != 1 || sink.closed.Load() != 0 {
		t.Fatalf("partial acquire cleanup = source %d, sink %d", source.closed.Load(), sink.closed.Load())
	}
}

func TestPrepareAcquiresOutputOnlyAfterResourceReservation(t *testing.T) {
	source, sink, instance, request := providerSessionFixture(t)
	policy := request.Policy()
	policy.Resources = job.ResourcePolicy{Limited: true, Limit: resource.Grant{}, Queue: policy.Resources.Queue}
	graph, _ := request.Graph()
	limited, err := job.New(request.Inputs(), request.Outputs(), graph, job.WithPolicy(policy), job.WithBudget(request.Budget()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = instance.Prepare(context.Background(), limited)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Phase != ResourcePhase {
		t.Fatalf("resource error = %v", err)
	}
	if source.acquired.Load() != 1 || source.closed.Load() != 1 {
		t.Fatalf("input sessions = %d/%d", source.acquired.Load(), source.closed.Load())
	}
	if sink.acquired.Load() != 0 || sink.closed.Load() != 0 {
		t.Fatalf("output acquired before reservation = %d/%d", sink.acquired.Load(), sink.closed.Load())
	}
}

func TestPreparedCloseReportsSessionFailure(t *testing.T) {
	want := errors.New("session close failed")
	_, sink, instance, request := providerSessionFixture(t)
	sink.closeErr = want
	prepared, err := instance.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(); !errors.Is(err, want) {
		t.Fatalf("Close error = %v", err)
	}
}

func TestPrepareDiagnosesActualCapabilityAndViewMismatch(t *testing.T) {
	tests := map[string]struct {
		configure func(*sessionCounters)
		code      string
	}{
		"capability": {
			configure: func(state *sessionCounters) { state.actual = mustCapabilities(t, access.RandomRead) },
			code:      "prepare.access-capabilities",
		},
		"view": {
			configure: func(state *sessionCounters) { state.noViews = true },
			code:      "prepare.access-view",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			source, _, instance, request := providerSessionFixture(t)
			test.configure(source)
			_, err := instance.Prepare(context.Background(), request)
			items := diagnostic.ItemsOf(err)
			if len(items) != 1 || items[0].Code != test.code || items[0].Detail["scheme"] != "memory" || items[0].Detail["selected"] != "sequential-read" {
				t.Fatalf("diagnostic %s = %#v, error %v", test.code, items, err)
			}
			if source.closed.Load() != 1 {
				t.Fatalf("failed session closed %d times", source.closed.Load())
			}
		})
	}
}

func TestPrepareRejectsMissingStableSizeViewBeforeSinkAcquire(t *testing.T) {
	sourceCapabilities := mustCapabilities(t, access.RandomRead, access.StableSize)
	sinkCapabilities := mustCapabilities(t, access.SequentialWrite)
	sourceState, sinkState, instance, request := providerSessionFixtureWith(
		t,
		sourceCapabilities,
		sinkCapabilities,
		access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)),
	)
	sourceState.noSize = true

	_, err := instance.Prepare(context.Background(), request)
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "prepare.access-view" || items[0].Detail["selected"] != "random-read,stable-size" || !strings.Contains(items[0].Detail["error"], "stable-size") {
		t.Fatalf("stable-size diagnostic = %#v, error %v", items, err)
	}
	if sourceState.closed.Load() != 1 || sinkState.acquired.Load() != 0 {
		t.Fatalf("sessions = source closed %d, sink acquired %d", sourceState.closed.Load(), sinkState.acquired.Load())
	}
}

func TestPrepareRejectsStableSizeSourceWithoutSnapshotIdentity(t *testing.T) {
	for name, configure := range map[string]func(*sessionCounters){
		"missing-snapshotter": func(state *sessionCounters) { state.noSnapshot = true },
		"no-snapshot-result":  func(state *sessionCounters) { state.snapshotErr = true },
	} {
		t.Run(name, func(t *testing.T) {
			sourceCapabilities := mustCapabilities(t, access.RandomRead, access.StableSize)
			sinkCapabilities := mustCapabilities(t, access.SequentialWrite)
			sourceState, sinkState, instance, request := providerSessionFixtureWith(
				t,
				sourceCapabilities,
				sinkCapabilities,
				access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)),
			)
			configure(sourceState)
			_, err := instance.Prepare(context.Background(), request)
			items := diagnostic.ItemsOf(err)
			if len(items) != 1 || items[0].Code != "prepare.access-snapshot" || items[0].Detail["required"] != string(access.StableSize) || items[0].Detail["error"] != access.ErrNoSnapshot.Error() {
				t.Fatalf("stable snapshot diagnostic = %#v, error %v", items, err)
			}
			if sourceState.closed.Load() != 1 || sinkState.acquired.Load() != 0 {
				t.Fatalf("sessions = source closed %d, sink acquired %d", sourceState.closed.Load(), sinkState.acquired.Load())
			}
		})
	}
}

func TestPrepareRequiresSnapshotterFromActualCapabilitiesForAutomaticSelection(t *testing.T) {
	sourceCapabilities := mustCapabilities(t, access.RandomRead, access.StableSize)
	sourceState := &sessionCounters{noSnapshot: true}
	reference, err := access.Parse("memory:data")
	if err != nil {
		t.Fatal(err)
	}
	projection := plan.Boundary{
		Direction:            plan.InputBoundary,
		Kind:                 plan.ProviderBoundary,
		Choice:               0,
		Node:                 "source",
		Port:                 "out",
		Component:            "source",
		Scheme:               reference.Scheme(),
		Reference:            reference.Display(),
		ReferenceFingerprint: reference.Fingerprint().String(),
		Available:            sourceCapabilities.Values(),
		Effective:            sourceCapabilities.Values(),
		// Automatic Probe starts with a read-only view. StableSize is
		// selected only after the Format requirements are finalized.
		Selected: []access.Capability{access.RandomRead},
	}
	sourceComponent, _, _, _ := boundaryComponentsWithRequirements(
		nil,
		[]plugin.ComponentOption{access.Source("memory", sourceCapabilities, sourceState.acquire(sourceCapabilities))},
		nil,
		access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)),
	)
	sourceTrait, ok := access.SourceOf(sourceComponent)
	if !ok {
		t.Fatal("automatic source fixture has no source trait")
	}
	entry := bound.AutomaticSource(projection, reference, sourceTrait)
	sessions, acquireErr := acquireSessions(context.Background(), []bound.Entry{entry}, plan.InputBoundary)
	if len(sessions) != 1 {
		t.Fatalf("acquired sessions = %d, want one session retained for cleanup", len(sessions))
	}
	items := diagnostic.ItemsOf(acquireErr)
	if len(items) != 1 || items[0].Code != "prepare.access-snapshot" || items[0].Detail["required"] != string(access.StableSize) || items[0].Detail["error"] != access.ErrNoSnapshot.Error() {
		t.Fatalf("automatic stable snapshot diagnostic = %#v, error %v", items, acquireErr)
	}
	if failures := closeSessions(context.Background(), sessions); len(failures) != 0 {
		t.Fatalf("source cleanup failures = %#v", failures)
	}

	_, transform, _, _ := boundaryComponentsWithRequirements(
		nil,
		nil,
		nil,
		access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)),
	)
	resolved, selection, finalizeErr := bind.FinalizeInput(
		entry,
		job.NewNode("format", transform.Identity(), config.NewPatch()),
		transform,
		sourceCapabilities,
	)
	if finalizeErr != nil {
		t.Fatal(finalizeErr)
	}
	if got := selection.Capabilities(); len(got) != 2 || got[0] != access.RandomRead || got[1] != access.StableSize || len(resolved.Projection().Selected) != 2 || resolved.Projection().Selected[0] != access.RandomRead || resolved.Projection().Selected[1] != access.StableSize {
		t.Fatalf("automatic final selection = %v / %v, want stable-size", got, resolved.Projection().Selected)
	}
	if sourceState.closed.Load() != 1 {
		t.Fatalf("source cleanup = %d, want one close", sourceState.closed.Load())
	}
}

func TestVerifySnapshotsSkipsExplicitNoSnapshot(t *testing.T) {
	sourceCapabilities := mustCapabilities(t, access.RandomRead)
	sourceState := &sessionCounters{}
	session := trackedAccessSession{capabilities: sourceCapabilities, counters: sourceState}
	noSnapshot, err := access.NewSnapshot("", access.NoSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if failure := verifySnapshots(context.Background(), RunPhase, []acquiredSession{{node: "source", value: session, snapshot: noSnapshot}}); failure != nil {
		t.Fatalf("NoSnapshot was treated as a changed identity: %v", failure)
	}
}

func providerSessionFixture(t *testing.T) (*sessionCounters, *sessionCounters, *Host, job.Job) {
	t.Helper()
	return providerSessionFixtureWith(
		t,
		mustCapabilities(t, access.SequentialRead),
		mustCapabilities(t, access.SequentialWrite),
		access.NewRequirements(access.AllOf(access.SequentialRead), access.AllOf(access.RandomRead)),
	)
}

func providerSessionFixtureWith(t *testing.T, sourceCapabilities, sinkCapabilities access.Capabilities, requirements access.Requirements) (*sessionCounters, *sessionCounters, *Host, job.Job) {
	t.Helper()
	sourceState := &sessionCounters{}
	sinkState := &sessionCounters{}
	source, transform, sink, _ := boundaryComponentsWithRequirements(
		nil,
		[]plugin.ComponentOption{access.Source("memory", sourceCapabilities, sourceState.acquire(sourceCapabilities))},
		[]plugin.ComponentOption{access.Sink("memory", sinkCapabilities, access.AtomicReplace, sinkState.acquire(sinkCapabilities))},
		requirements,
	)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "session fixture", Version: "1"}, source, transform, sink))
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
	return sourceState, sinkState, instance, request
}
