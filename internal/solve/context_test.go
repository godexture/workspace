package solve

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type solveContextSourceID struct{}
type solveContextSinkID struct{}
type solveContextBridgeID struct{}
type solveContextKey struct{}

type compileDeadlineLog struct {
	mu      sync.Mutex
	entries map[string][]time.Time
}

func (l *compileDeadlineLog) observe(name string, ctx plugin.CompileContext) error {
	if value := ctx.Context().Value(solveContextKey{}); value != nil {
		return fmt.Errorf("%s Compile received context value %v", name, value)
	}
	deadline, ok := ctx.Context().Deadline()
	if !ok {
		return fmt.Errorf("%s Compile received no deadline", name)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[string][]time.Time)
	}
	l.entries[name] = append(l.entries[name], deadline)
	return nil
}

func (l *compileDeadlineLog) saw(name string, deadline time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, value := range l.entries[name] {
		if value.Equal(deadline) {
			return true
		}
	}
	return false
}

func awaitCompileCancellation(ctx plugin.CompileContext, started chan struct{}, once *sync.Once, hidden, canceled *atomic.Bool) error {
	hidden.Store(ctx.Context().Value(solveContextKey{}) == nil)
	once.Do(func() { close(started) })
	select {
	case <-ctx.Context().Done():
		canceled.Store(errors.Is(ctx.Context().Err(), context.Canceled))
		return ctx.Context().Err()
	case <-time.After(time.Second):
		return errors.New("Compile cancellation was not propagated")
	}
}

func cancelWhenCompileStarts(started <-chan struct{}, cancel context.CancelFunc) {
	select {
	case <-started:
	case <-time.After(time.Second):
	}
	cancel()
}

func solveContextSource(observe func(plugin.CompileContext) error) plugin.Component {
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", solveSchemaA)})
	return solveContextComponent[solveContextSourceID](shape, func(ctx plugin.CompileContext, _ solveConfig, _ flow.Descriptors[stream.Descriptor]) (plugin.Compiled[solvePlan, stream.Descriptor], error) {
		if err := observe(ctx); err != nil {
			return plugin.Compiled[solvePlan, stream.Descriptor]{}, err
		}
		return plugin.Compiled[solvePlan, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", solveDescriptor(solveSchemaA, 44100)))}, nil
	}, nil, 0, plugin.Contract{}, nil, nil)
}

func solveContextSink(observe func(plugin.CompileContext) error) plugin.Component {
	shape := flow.NewShape([]flow.Port{flow.In("in", solveSchemaB)}, nil)
	return solveContextComponent[solveContextSinkID](shape, func(ctx plugin.CompileContext, _ solveConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[solvePlan, stream.Descriptor], error) {
		if err := observe(ctx); err != nil {
			return plugin.Compiled[solvePlan, stream.Descriptor]{}, err
		}
		if _, ok := inputs.One("in"); !ok {
			return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("fixture.input"))}}, nil
		}
		return plugin.Compiled[solvePlan, stream.Descriptor]{Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
	}, nil, 0, plugin.Contract{}, nil, nil)
}

func solveContextBridge(observe func(plugin.CompileContext) error) plugin.Component {
	shape := flow.NewShape([]flow.Port{flow.In("in", solveSchemaA)}, []flow.Port{flow.Out("out", solveSchemaB)})
	return solveContextComponent[solveContextBridgeID](shape, func(ctx plugin.CompileContext, value solveConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[solvePlan, stream.Descriptor], error) {
		if err := observe(ctx); err != nil {
			return plugin.Compiled[solvePlan, stream.Descriptor]{}, err
		}
		input, ok := inputs.One("in")
		if !ok {
			return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("fixture.input"))}}, nil
		}
		return plugin.Compiled[solvePlan, stream.Descriptor]{
			Outputs:   flow.NewDescriptors(flow.Describe("out", schemaTransform(solveSchemaB)(input, value))),
			Effects:   []plugin.Effect{structural("ab")},
			Resources: resource.Request{Memory: 1},
		}, nil
	}, nil, 0, plugin.Contract{}, nil, nil)
}

func TestResolveCompileContextDeadlineDoesNotChangePlanIdentity(t *testing.T) {
	log := &compileDeadlineLog{}
	source := solveContextSource(func(ctx plugin.CompileContext) error { return log.observe("source", ctx) })
	sink := solveContextSink(func(ctx plugin.CompileContext) error { return log.observe("sink", ctx) })
	bridge := solveContextBridge(func(ctx plugin.CompileContext) error { return log.observe("bridge", ctx) })
	index := solveIndex(t, source, sink, bridge)
	request := solveRequest(t, source, sink, job.DefaultBudget())

	firstDeadline := time.Now().Add(time.Hour)
	firstContext, cancelFirst := context.WithDeadline(context.WithValue(context.Background(), solveContextKey{}, "first"), firstDeadline)
	first, err := Resolve(firstContext, index, request, solvePlatform())
	cancelFirst()
	if err != nil {
		t.Fatal(err)
	}
	secondDeadline := firstDeadline.Add(time.Hour)
	secondContext, cancelSecond := context.WithDeadline(context.WithValue(context.Background(), solveContextKey{}, "second"), secondDeadline)
	second, err := Resolve(secondContext, index, request, solvePlatform())
	cancelSecond()
	if err != nil {
		t.Fatal(err)
	}

	if first.Plan().Fingerprint() != second.Plan().Fingerprint() || first.Plan().ExecutionSignature() != second.Plan().ExecutionSignature() {
		t.Fatal("Compile deadline changed Plan identity")
	}
	for _, name := range []string{"source", "sink", "bridge"} {
		if !log.saw(name, firstDeadline) || !log.saw(name, secondDeadline) {
			t.Fatalf("%s Compile did not receive both planning deadlines", name)
		}
	}
}

func TestResolvePropagatesCancellationToRequestedCompile(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	var hidden, canceled atomic.Bool
	source := solveContextSource(func(ctx plugin.CompileContext) error {
		return awaitCompileCancellation(ctx, started, &once, &hidden, &canceled)
	})
	sink := solveSink(solveSchemaA, false, nil)
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), solveContextKey{}, "hidden"))
	go cancelWhenCompileStarts(started, cancel)

	_, err := Resolve(ctx, solveIndex(t, source, sink), solveRequest(t, source, sink, job.DefaultBudget()), solvePlatform())
	if err == nil {
		t.Fatal("Resolve succeeded after requested Compile cancellation")
	}
	if !hidden.Load() || !canceled.Load() {
		t.Fatalf("requested Compile observed hidden=%v canceled=%v", hidden.Load(), canceled.Load())
	}
}

func TestResolvePropagatesCancellationToAutomaticCompile(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	var hidden, canceled atomic.Bool
	source := solveSource(solveSchemaA, nil)
	sink := solveSink(solveSchemaB, false, nil)
	bridge := solveContextBridge(func(ctx plugin.CompileContext) error {
		return awaitCompileCancellation(ctx, started, &once, &hidden, &canceled)
	})
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), solveContextKey{}, "hidden"))
	go cancelWhenCompileStarts(started, cancel)

	_, err := Resolve(ctx, solveIndex(t, source, sink, bridge), solveRequest(t, source, sink, job.DefaultBudget()), solvePlatform())
	if err == nil {
		t.Fatal("Resolve succeeded after automatic Compile cancellation")
	}
	if !hidden.Load() || !canceled.Load() {
		t.Fatalf("automatic Compile observed hidden=%v canceled=%v", hidden.Load(), canceled.Load())
	}
}

func TestResolveClassifiesDirectDurationExhaustion(t *testing.T) {
	source := solveContextSource(func(ctx plugin.CompileContext) error {
		<-ctx.Context().Done()
		return ctx.Context().Err()
	})
	sink := solveSink(solveSchemaA, false, nil)
	budget := job.DefaultBudget()
	budget.Duration = time.Millisecond
	_, err := Resolve(t.Context(), solveIndex(t, source, sink), solveRequest(t, source, sink, budget), solvePlatform())
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "solve.budget-exhausted" || items[0].Detail["dimension"] != "duration" {
		t.Fatalf("duration diagnostic = %#v, error=%v", items, err)
	}
}
