package host

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"
)

type sessionCounters struct {
	acquired atomic.Int32
	closed   atomic.Int32
	fail     error
	closeErr error
}

type trackedAccessSession struct {
	capabilities access.Capabilities
	counters     *sessionCounters
}

func (s trackedAccessSession) Capabilities() access.Capabilities { return s.capabilities }
func (s trackedAccessSession) Close() error {
	s.counters.closed.Add(1)
	return s.counters.closeErr
}

func (c *sessionCounters) acquire(capabilities access.Capabilities) access.AcquireFunc {
	return func(context.Context, access.Reference, access.Selection) (access.Session, error) {
		c.acquired.Add(1)
		if c.fail != nil {
			return nil, c.fail
		}
		return trackedAccessSession{capabilities: capabilities, counters: c}, nil
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
	source, _, sink, _ := boundaryComponentsWith(
		nil,
		[]plugin.ComponentOption{access.Source("memory", sourceCapabilities, sourceState.acquire(sourceCapabilities))},
		[]plugin.ComponentOption{access.Sink("memory", sinkCapabilities, access.AtomicReplace, sinkState.acquire(sinkCapabilities))},
	)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "session fixture", Version: "1"}, source, sink))
	instance, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	inputReference, _ := access.Parse("memory:input")
	outputReference, _ := access.Parse("memory:output")
	input, _ := job.InputFromReference(inputReference)
	output, _ := job.OutputToReference(outputReference)
	request, err := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
	if err != nil {
		t.Fatal(err)
	}
	return sourceState, sinkState, instance, request
}
