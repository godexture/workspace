package host

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type sessionCounters struct {
	acquired atomic.Int32
	closed   atomic.Int32
	fail     error
	closeErr error
	actual   access.Capabilities
	noViews  bool
	wait     bool
}

type trackedAccessSession struct {
	capabilities access.Capabilities
	counters     *sessionCounters
}

type capabilityOnlyAccessSession struct {
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
		return trackedAccessSession{capabilities: actual, counters: c}, nil
	}
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

func providerSessionFixture(t *testing.T) (*sessionCounters, *sessionCounters, *Host, job.Job) {
	t.Helper()
	sourceCapabilities, err := access.NewCapabilities(access.SequentialRead)
	if err != nil {
		t.Fatal(err)
	}
	sinkCapabilities, err := access.NewCapabilities(access.SequentialWrite)
	if err != nil {
		t.Fatal(err)
	}
	sourceState := &sessionCounters{}
	sinkState := &sessionCounters{}
	source, transform, sink, _ := boundaryComponentsWith(
		nil,
		[]plugin.ComponentOption{access.Source("memory", sourceCapabilities, sourceState.acquire(sourceCapabilities))},
		[]plugin.ComponentOption{access.Sink("memory", sinkCapabilities, access.AtomicReplace, sinkState.acquire(sinkCapabilities))},
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
