package host

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type (
	lifecyclePluginID    struct{}
	lifecycleConfigID    struct{}
	lifecycleSourceID    struct{}
	lifecycleProcessorID struct{}
	lifecycleSinkID      struct{}
	lifecycleSinkBID     struct{}
	lifecycleSchemaID    struct{}
	lifecycleConfig      struct{}
)

var lifecycleType = schema.Define[lifecycleSchemaID](schema.Traits[int]{})

type lifecycleState struct {
	mu             sync.Mutex
	entries        []string
	values         []int
	fail           map[string]error
	task           func(context.Context) error
	block          bool
	bound          bool
	multi          bool
	inputEndpoint  endpoint.Opening
	outputEndpoint endpoint.Opening
	panicAt        string
	direct         bool
	sourceHandle   *lifecycleHandle
	sinkHandle     *lifecycleHandle
}

type lifecycleHandle struct{ closed atomic.Int32 }

func (h *lifecycleHandle) Close() error {
	h.closed.Add(1)
	return nil
}

func (s *lifecycleState) add(value string) {
	s.mu.Lock()
	s.entries = append(s.entries, value)
	s.mu.Unlock()
}

func (s *lifecycleState) failure(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fail[name]
}

func (s *lifecycleState) snapshot() ([]string, []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.entries...), append([]int(nil), s.values...)
}

func (s *lifecycleState) panicIf(name string) {
	if s.panicAt == name {
		panic(name)
	}
}

type lifecycleBase struct {
	shape flow.Shape
	name  string
	state *lifecycleState
}

func (o *lifecycleBase) Ports() flow.Shape { return o.shape.Clone() }
func (o *lifecycleBase) Close() error {
	o.state.add("close/" + o.name)
	o.state.panicIf("close/" + o.name)
	return o.state.failure("close/" + o.name)
}

type lifecycleSource struct {
	*lifecycleBase
	index int
}

func (s *lifecycleSource) Read(ctx context.Context, into *flow.Item[int]) error {
	if s.state.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if s.index == 3 {
		s.state.add("eof/source")
		return io.EOF
	}
	value := s.index + 1
	s.index++
	s.state.add("read/source")
	into.Set(value)
	return nil
}

type lifecycleProcessor struct{ *lifecycleBase }

func (p *lifecycleProcessor) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	p.state.add("process/processor")
	p.state.panicIf("process/processor")
	item := flow.NewItem(input.Value()*2, lifecycleType, &testDomain)
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	input.Drop()
	return nil
}

func (p *lifecycleProcessor) Finalize(context.Context) error {
	p.state.add("finalize/processor")
	p.state.panicIf("finalize/processor")
	return p.state.failure("finalize/processor")
}

func (p *lifecycleProcessor) Flush(context.Context, flow.Emitter[int]) error {
	p.state.add("flow-flush/processor")
	p.state.panicIf("flow-flush/processor")
	return p.state.failure("flow-flush/processor")
}

type lifecycleSink struct{ *lifecycleBase }

func (s *lifecycleSink) Write(_ context.Context, input *flow.Item[int]) error {
	phase := "write/" + s.name
	s.state.add(phase)
	if err := s.state.failure(phase); err != nil {
		return err
	}
	s.state.mu.Lock()
	s.state.values = append(s.state.values, input.Value())
	s.state.mu.Unlock()
	input.Drop()
	return nil
}

func (s *lifecycleSink) Flush(context.Context) error {
	phase := "flush/" + s.name
	s.state.add(phase)
	return s.state.failure(phase)
}

func (s *lifecycleSink) Sync(context.Context) error {
	phase := "sync/" + s.name
	s.state.add(phase)
	return s.state.failure(phase)
}

func (s *lifecycleSink) PrepareCommit(context.Context) error {
	phase := "prepare-commit/" + s.name
	s.state.add(phase)
	return s.state.failure(phase)
}

func (s *lifecycleSink) Commit(context.Context) error {
	phase := "commit/" + s.name
	s.state.add(phase)
	return s.state.failure(phase)
}

func (s *lifecycleSink) Abort(context.Context) error {
	phase := "abort/" + s.name
	s.state.add(phase)
	return s.state.failure(phase)
}

func lifecycleFixture(t *testing.T, state *lifecycleState, options ...Option) (*Host, job.Job) {
	t.Helper()
	if state.fail == nil {
		state.fail = make(map[string]error)
	}
	configuration := config.Struct[lifecycleConfigID](func() lifecycleConfig { return lifecycleConfig{} }).Version("1").Build()
	descriptor := stream.MustDescriptor("fixture", lifecycleType.Identity(), timing.MustBase(1, 1000), property.New())
	sourcePort := flow.Out("out", lifecycleType)
	if state.multi {
		sourcePort = flow.Out("out", lifecycleType, flow.Many())
	}
	sourceShape := flow.NewShape(nil, []flow.Port{sourcePort})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", lifecycleType)}, []flow.Port{flow.Out("out", lifecycleType)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", lifecycleType)}, nil)
	var sourceTraits, sinkTraits []plugin.ComponentOption
	if state.bound {
		trait, err := endpoint.NewTrait(endpoint.FiniteStatic, endpoint.Offline)
		if err != nil {
			t.Fatal(err)
		}
		sourceTraits = append(sourceTraits, endpoint.WithTrait(trait))
		sinkTraits = append(sinkTraits, endpoint.WithTrait(trait))
	}

	source := plugin.NewComponent[lifecycleSourceID](
		plugin.Descriptor{DisplayName: "source", Version: "1"},
		configuration,
		append([]plugin.ComponentOption{plugin.WithSpec(plugin.Spec[lifecycleConfig, flow.Shape, stream.Descriptor]{
			Shape: plugin.StaticShape[lifecycleConfig](sourceShape),
			Compile: func(plugin.CompileContext, lifecycleConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
				resources := resource.Request{}
				if state.task != nil {
					resources.Workers = 1
				}
				return plugin.Compiled[flow.Shape, stream.Descriptor]{Plan: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor)), Resources: resources}, nil
			},
			Open: func(ctx plugin.OpenContext, shape flow.Shape) (flow.Operator, error) {
				state.add("open/source")
				state.panicIf("open/source")
				if err := state.failure("open/source"); err != nil {
					return nil, err
				}
				if state.bound {
					opening, ok := plugin.Boundary[endpoint.Opening](ctx)
					if !ok || !opening.Valid() {
						return nil, errors.New("source endpoint opening is missing")
					}
					state.inputEndpoint = opening
				}
				if state.direct {
					opening, ok := plugin.Boundary[access.Direct[*lifecycleHandle]](ctx)
					if !ok || !opening.Valid() || opening.Value() != state.sourceHandle {
						return nil, errors.New("source direct opening is missing")
					}
				}
				if state.task != nil {
					if err := ctx.Tasks().Start("fixture/background", state.task); err != nil {
						return nil, err
					}
				}
				return &lifecycleSource{lifecycleBase: &lifecycleBase{shape: shape, name: "source", state: state}}, nil
			},
		}),
			plugin.WithReader("out", lifecycleType),
		}, sourceTraits...)...,
	)
	processor := plugin.NewComponent[lifecycleProcessorID](
		plugin.Descriptor{DisplayName: "processor", Version: "1"},
		configuration,
		plugin.WithSpec(plugin.Spec[lifecycleConfig, flow.Shape, stream.Descriptor]{
			Shape: plugin.StaticShape[lifecycleConfig](processorShape),
			Compile: func(_ plugin.CompileContext, _ lifecycleConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
				input, _ := inputs.One("in")
				return plugin.Compiled[flow.Shape, stream.Descriptor]{
					Plan:         processorShape,
					Outputs:      flow.NewDescriptors(flow.Describe("out", input)),
					Finalization: plugin.RequiresFinalization,
				}, nil
			},
			Open: func(_ plugin.OpenContext, shape flow.Shape) (flow.Operator, error) {
				state.add("open/processor")
				state.panicIf("open/processor")
				if err := state.failure("open/processor"); err != nil {
					return nil, err
				}
				return &lifecycleProcessor{lifecycleBase: &lifecycleBase{shape: shape, name: "processor", state: state}}, nil
			},
			Finalizes: true,
		}),
		plugin.WithProcessor("in", lifecycleType, "out", lifecycleType),
	)
	sinkOption := func(name string) plugin.ComponentOption {
		return plugin.WithSpec(plugin.Spec[lifecycleConfig, flow.Shape, stream.Descriptor]{
			Shape: plugin.StaticShape[lifecycleConfig](sinkShape),
			Compile: func(plugin.CompileContext, lifecycleConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
				return plugin.Compiled[flow.Shape, stream.Descriptor]{Plan: sinkShape, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
			},
			Open: func(ctx plugin.OpenContext, shape flow.Shape) (flow.Operator, error) {
				state.add("open/" + name)
				state.panicIf("open/" + name)
				if err := state.failure("open/" + name); err != nil {
					return nil, err
				}
				if state.bound {
					opening, ok := plugin.Boundary[endpoint.Opening](ctx)
					if !ok || !opening.Valid() {
						return nil, errors.New("sink endpoint opening is missing")
					}
					state.outputEndpoint = opening
				}
				if state.direct {
					opening, ok := plugin.Boundary[access.Direct[*lifecycleHandle]](ctx)
					if !ok || !opening.Valid() || opening.Value() != state.sinkHandle {
						return nil, errors.New("sink direct opening is missing")
					}
				}
				return &lifecycleSink{lifecycleBase: &lifecycleBase{shape: shape, name: name, state: state}}, nil
			},
		})
	}
	sink := plugin.NewComponent[lifecycleSinkID](
		plugin.Descriptor{DisplayName: "sink", Version: "1"},
		configuration,
		append([]plugin.ComponentOption{sinkOption("sink"), plugin.WithWriter("in", lifecycleType)}, sinkTraits...)...,
	)
	components := []plugin.Component{source, processor, sink}
	var sinkB plugin.Component
	if state.multi {
		sinkB = plugin.NewComponent[lifecycleSinkBID](
			plugin.Descriptor{DisplayName: "sink b", Version: "1"},
			configuration,
			sinkOption("sink-b"),
			plugin.WithWriter("in", lifecycleType),
		)
		components = append(components, sinkB)
	}
	definition := plugin.Define[lifecyclePluginID](plugin.Descriptor{DisplayName: "lifecycle", Version: "1"}, components...)
	hostOptions := []Option{
		Plugins(plugin.NewSet(definition)),
		PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
	}
	hostOptions = append(hostOptions, options...)
	instance, err := New(hostOptions...)
	if err != nil {
		t.Fatal(err)
	}
	if state.direct {
		state.sourceHandle = &lifecycleHandle{}
		state.sinkHandle = &lifecycleHandle{}
		sourceAdaptor, adaptorErr := job.NewAdaptor(source.Identity(), config.NewPatch())
		if adaptorErr != nil {
			t.Fatal(adaptorErr)
		}
		sinkAdaptor, adaptorErr := job.NewAdaptor(sink.Identity(), config.NewPatch())
		if adaptorErr != nil {
			t.Fatal(adaptorErr)
		}
		input, inputErr := job.InputFromSource(access.Own(state.sourceHandle), sourceAdaptor)
		if inputErr != nil {
			t.Fatal(inputErr)
		}
		output, outputErr := job.OutputToSink(access.Own(state.sinkHandle), sinkAdaptor)
		if outputErr != nil {
			t.Fatal(outputErr)
		}
		request, requestErr := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		return instance, request
	}
	if state.bound {
		inputRequest, inputErr := job.NewEndpoint(source.Identity(), config.NewPatch())
		if inputErr != nil {
			t.Fatal(inputErr)
		}
		input, inputErr := job.InputFromEndpoint(inputRequest)
		if inputErr != nil {
			t.Fatal(inputErr)
		}
		outputRequest, outputErr := job.NewEndpoint(sink.Identity(), config.NewPatch())
		if outputErr != nil {
			t.Fatal(outputErr)
		}
		output, outputErr := job.OutputToEndpoint(outputRequest)
		if outputErr != nil {
			t.Fatal(outputErr)
		}
		request, requestErr := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		return instance, request
	}
	nodes := []job.Node{
		job.NewNode("source", source.Identity(), config.NewPatch()),
		job.NewNode("processor", processor.Identity(), config.NewPatch()),
		job.NewNode("sink", sink.Identity(), config.NewPatch()),
	}
	edges := []job.Edge{
		job.Connect(job.At("source", "out"), job.At("processor", "in")),
		job.Connect(job.At("processor", "out"), job.At("sink", "in")),
	}
	if state.multi {
		nodes = append(nodes, job.NewNode("sink-b", sinkB.Identity(), config.NewPatch()))
		edges = append(edges, job.Connect(job.At("source", "out"), job.At("sink-b", "in")))
	}
	graph, err := job.NewGraph(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, graph)
	if err != nil {
		t.Fatal(err)
	}
	return instance, request
}

func TestPreparedRunHandsNodeLocalEndpointOpeningsToComponents(t *testing.T) {
	state := &lifecycleState{bound: true}
	instance, request := lifecycleFixture(t, state)
	result, err := instance.Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if state.inputEndpoint.Direction() != endpoint.SourceDirection || state.inputEndpoint.Trait().Topology() != endpoint.FiniteStatic {
		t.Fatalf("input endpoint opening = %#v", state.inputEndpoint)
	}
	if state.outputEndpoint.Direction() != endpoint.SinkDirection || state.outputEndpoint.Trait().Topology() != endpoint.FiniteStatic {
		t.Fatalf("output endpoint opening = %#v", state.outputEndpoint)
	}
	if len(result.Outputs) != 1 || result.Outputs[0].Class != 0 || result.Outputs[0].State != OutputCommitted {
		t.Fatalf("output = %#v", result.Outputs)
	}
}

func TestPreparedRunOwnsDirectResourcesAndUsesExplicitAdaptors(t *testing.T) {
	state := &lifecycleState{direct: true}
	instance, request := lifecycleFixture(t, state)
	prepared, err := instance.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	boundaries := prepared.Plan().Boundaries()
	if len(boundaries) != 2 || boundaries[0].Kind != plan.DirectBoundary || boundaries[0].Ownership != access.Owned || boundaries[1].Kind != plan.DirectBoundary || boundaries[1].Ownership != access.Owned {
		t.Fatalf("direct boundaries = %#v", boundaries)
	}
	result, err := prepared.Run(context.Background())
	if err != nil || !result.Succeeded() {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if state.sourceHandle.closed.Load() != 1 || state.sinkHandle.closed.Load() != 1 {
		t.Fatalf("direct close counts = source %d sink %d", state.sourceHandle.closed.Load(), state.sinkHandle.closed.Load())
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if state.sourceHandle.closed.Load() != 1 || state.sinkHandle.closed.Load() != 1 {
		t.Fatal("idempotent Prepared.Close closed direct resources twice")
	}
}

func TestPreparedCloseBeforeRunReleasesDirectResources(t *testing.T) {
	state := &lifecycleState{direct: true}
	instance, request := lifecycleFixture(t, state)
	prepared, err := instance.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	if state.sourceHandle.closed.Load() != 1 || state.sinkHandle.closed.Load() != 1 {
		t.Fatalf("direct close counts = source %d sink %d", state.sourceHandle.closed.Load(), state.sinkHandle.closed.Load())
	}
	if entries, _ := state.snapshot(); len(entries) != 0 {
		t.Fatalf("Close before Run opened operators: %v", entries)
	}
}

func resultPlan(t *testing.T, instance *Host, request job.Job) plan.Plan {
	t.Helper()
	result, err := instance.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPreparedRunOrdersFinalizeFlushAndCommit(t *testing.T) {
	state := &lifecycleState{}
	instance, request := lifecycleFixture(t, state)
	prepared, err := instance.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.Plan().Valid() {
		t.Fatal("prepared Plan is invalid")
	}
	if entries, _ := state.snapshot(); len(entries) != 0 {
		t.Fatalf("Prepare opened runtime state: %v", entries)
	}
	result, err := prepared.Run(context.Background(), Observe(ObservationBasic, RetainEvents(64)))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded() || len(result.Outputs) != 1 || result.Outputs[0].State != OutputCommitted {
		t.Fatalf("result = %#v", result)
	}
	entries, values := state.snapshot()
	if len(values) != 3 || values[0] != 2 || values[1] != 4 || values[2] != 6 {
		t.Fatalf("sink values = %v", values)
	}
	assertOrder(t, entries,
		"open/sink", "open/source", "open/processor",
		"eof/source", "finalize/processor", "flow-flush/processor",
		"flush/sink", "sync/sink", "prepare-commit/sink", "commit/sink",
		"close/processor", "close/source", "close/sink",
	)
	finalizeIndex := indexOf(entries, "finalize/processor")
	for index, entry := range entries {
		if index > finalizeIndex && (entry == "process/processor" || entry == "write/sink") {
			t.Fatalf("Finalize raced ordinary processing: %v", entries)
		}
	}
	if len(result.Events) == 0 {
		t.Fatal("Basic observation returned no lifecycle/progress events")
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedRunRollsBackPartialOpenInReverseOrder(t *testing.T) {
	openFailure := errors.New("processor open failed")
	state := &lifecycleState{fail: map[string]error{"open/processor": openFailure}}
	instance, request := lifecycleFixture(t, state)
	result, err := instance.Run(context.Background(), request)
	if err == nil || result.Primary == nil || result.Primary.Phase != OpenPhase {
		t.Fatalf("primary = %#v, err = %v", result.Primary, err)
	}
	if len(result.Outputs) != 1 || result.Outputs[0].State != OutputAborted {
		t.Fatalf("outputs = %#v", result.Outputs)
	}
	entries, _ := state.snapshot()
	assertOrder(t, entries, "open/sink", "open/source", "open/processor", "abort/sink", "close/source", "close/sink")
}

func TestPreparedRunSeparatesCommitAbortAndCloseFailures(t *testing.T) {
	commitFailure := errors.New("commit failed")
	abortFailure := errors.New("abort failed")
	closeFailure := errors.New("close failed")
	state := &lifecycleState{fail: map[string]error{
		"commit/sink": commitFailure,
		"abort/sink":  abortFailure,
		"close/sink":  closeFailure,
	}}
	instance, request := lifecycleFixture(t, state)
	result, err := instance.Run(context.Background(), request)
	if err == nil || result.Primary == nil || result.Primary.Phase != CommitPhase || !errors.Is(result.Primary, commitFailure) {
		t.Fatalf("primary = %#v, err = %v", result.Primary, err)
	}
	if len(result.Cleanup) != 2 || len(result.Outputs) != 1 || result.Outputs[0].State != OutputUnknown || !result.Outputs[0].RollbackAttempted {
		t.Fatalf("result = %#v", result)
	}
	if result.Cleanup[0].Phase != AbortPhase || result.Cleanup[1].Phase != ClosePhase {
		t.Fatalf("cleanup = %#v", result.Cleanup)
	}
}

func TestPreparedRunReportsPartialMultiOutputCommit(t *testing.T) {
	state := &lifecycleState{multi: true, fail: map[string]error{"commit/sink-b": errors.New("second commit failed")}}
	instance, request := lifecycleFixture(t, state)
	result, err := instance.Run(context.Background(), request)
	if err == nil || result.Primary == nil || result.Primary.Phase != CommitPhase {
		t.Fatalf("primary = %#v, err = %v", result.Primary, err)
	}
	if len(result.Outputs) != 2 || result.Outputs[0].Node != "sink" || result.Outputs[0].State != OutputCommitted || result.Outputs[1].Node != "sink-b" || result.Outputs[1].State != OutputUnknown || !result.Outputs[1].RollbackAttempted {
		t.Fatalf("outputs = %#v", result.Outputs)
	}
	entries, _ := state.snapshot()
	assertOrder(t, entries,
		"prepare-commit/sink", "prepare-commit/sink-b",
		"commit/sink", "commit/sink-b", "abort/sink-b",
	)
}

func TestPrepareRejectsAggregateRuntimeResourcesBeforeOpen(t *testing.T) {
	state := &lifecycleState{}
	instance, request := lifecycleFixture(t, state)
	policy := request.Policy()
	policy.Resources = job.ResourcePolicy{Limited: true, Limit: resource.Grant{Queue: 7}, Queue: policy.Resources.Queue}
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
	if entries, _ := state.snapshot(); len(entries) != 0 {
		t.Fatalf("resource rejection opened operators: %v", entries)
	}
}

func TestPreparedRunDoesNotFlushAfterFinalizeFailure(t *testing.T) {
	state := &lifecycleState{fail: map[string]error{"finalize/processor": errors.New("finalize failed")}}
	instance, request := lifecycleFixture(t, state)
	result, err := instance.Run(context.Background(), request)
	if err == nil || result.Primary == nil || result.Primary.Phase != FinalizePhase || result.Outputs[0].State != OutputAborted {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	entries, _ := state.snapshot()
	if contains(entries, "flow-flush/processor") || contains(entries, "flush/sink") || contains(entries, "commit/sink") {
		t.Fatalf("failure path ran success finalization: %v", entries)
	}
}

func TestPreparedRunRetainsNodeAndStackForLifecyclePanic(t *testing.T) {
	for _, test := range []struct {
		name  string
		phase Phase
		node  string
	}{
		{name: "open/processor", phase: OpenPhase, node: "processor"},
		{name: "process/processor", phase: RunPhase, node: "processor"},
		{name: "finalize/processor", phase: FinalizePhase, node: "processor"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := &lifecycleState{panicAt: test.name}
			instance, request := lifecycleFixture(t, state)
			result, err := instance.Run(context.Background(), request)
			if err == nil || result.Primary == nil || result.Primary.Phase != test.phase || result.Primary.Node != test.node || len(result.Primary.Stack) == 0 {
				t.Fatalf("primary = %#v, err = %v", result.Primary, err)
			}
		})
	}
}

func TestPreparedRunReportsClosePanicAsCleanup(t *testing.T) {
	state := &lifecycleState{panicAt: "close/sink"}
	instance, request := lifecycleFixture(t, state)
	result, err := instance.Run(context.Background(), request)
	if err == nil || result.Primary != nil || len(result.Cleanup) != 1 || result.Cleanup[0].Phase != ClosePhase || result.Cleanup[0].Node != "sink" || len(result.Cleanup[0].Stack) == 0 {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestPreparedRunReportsUnjoinedPluginTaskWithoutClaimingItStopped(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	state := &lifecycleState{task: func(context.Context) error {
		close(started)
		<-release
		return nil
	}}
	instance, request := lifecycleFixture(t, state, CleanupTimeout(20*time.Millisecond))
	result, err := instance.Run(context.Background(), request)
	<-started
	close(release)
	if err == nil || result.Primary != nil || len(result.Cleanup) == 0 {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	found := false
	for _, failure := range result.Cleanup {
		found = found || failure.Phase == JoinPhase && failure.Task == "fixture/background"
	}
	if !found {
		t.Fatalf("unjoined task cleanup = %#v", result.Cleanup)
	}
}

func assertOrder(t *testing.T, values []string, expected ...string) {
	t.Helper()
	position := 0
	for _, value := range values {
		if position < len(expected) && value == expected[position] {
			position++
		}
	}
	if position != len(expected) {
		t.Fatalf("order %v does not contain %v", values, expected)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
