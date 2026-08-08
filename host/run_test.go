package host

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type (
	lifecyclePluginID    struct{}
	lifecycleConfigID    struct{}
	lifecycleSourceID    struct{}
	lifecycleProcessorID struct{}
	lifecycleSinkID      struct{}
	lifecycleSchemaID    struct{}
	lifecycleConfig      struct{}
)

var lifecycleType = schema.Define[lifecycleSchemaID](schema.Traits[int]{})

type lifecycleState struct {
	mu      sync.Mutex
	entries []string
	values  []int
	fail    map[string]error
	task    func(context.Context) error
	block   bool
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

type lifecycleBase struct {
	shape flow.Shape
	name  string
	state *lifecycleState
}

func (o *lifecycleBase) Ports() flow.Shape { return o.shape.Clone() }
func (o *lifecycleBase) Close() error {
	o.state.add("close/" + o.name)
	return o.state.failure("close/" + o.name)
}

type lifecycleSource struct {
	*lifecycleBase
	index int
}

func (s *lifecycleSource) Read(ctx context.Context) (flow.Input[int], error) {
	if s.state.block {
		<-ctx.Done()
		return flow.Input[int]{}, ctx.Err()
	}
	if s.index == 3 {
		s.state.add("eof/source")
		return flow.Input[int]{}, io.EOF
	}
	value := s.index + 1
	s.index++
	s.state.add("read/source")
	return flow.NewInput(value, lifecycleType), nil
}

type lifecycleProcessor struct{ *lifecycleBase }

func (p *lifecycleProcessor) Process(ctx context.Context, input flow.Input[int], output flow.Emitter[int]) error {
	p.state.add("process/processor")
	item := flow.NewInput(input.Value()*2, lifecycleType)
	if err := output.Emit(ctx, item); err != nil {
		item.Drop()
		return err
	}
	input.Drop()
	return nil
}

func (p *lifecycleProcessor) Finalize(context.Context) error {
	p.state.add("finalize/processor")
	return p.state.failure("finalize/processor")
}

func (p *lifecycleProcessor) Flush(context.Context, flow.Emitter[int]) error {
	p.state.add("flow-flush/processor")
	return p.state.failure("flow-flush/processor")
}

type lifecycleSink struct{ *lifecycleBase }

func (s *lifecycleSink) Write(_ context.Context, input flow.Input[int]) error {
	s.state.add("write/sink")
	if err := s.state.failure("write/sink"); err != nil {
		return err
	}
	s.state.mu.Lock()
	s.state.values = append(s.state.values, input.Value())
	s.state.mu.Unlock()
	input.Drop()
	return nil
}

func (s *lifecycleSink) Flush(context.Context) error {
	s.state.add("flush/sink")
	return s.state.failure("flush/sink")
}

func (s *lifecycleSink) Sync(context.Context) error {
	s.state.add("sync/sink")
	return s.state.failure("sync/sink")
}

func (s *lifecycleSink) PrepareCommit(context.Context) error {
	s.state.add("prepare-commit/sink")
	return s.state.failure("prepare-commit/sink")
}

func (s *lifecycleSink) Commit(context.Context) error {
	s.state.add("commit/sink")
	return s.state.failure("commit/sink")
}

func (s *lifecycleSink) Abort(context.Context) error {
	s.state.add("abort/sink")
	return s.state.failure("abort/sink")
}

func lifecycleFixture(t *testing.T, state *lifecycleState, options ...Option) (*Host, job.Job) {
	t.Helper()
	if state.fail == nil {
		state.fail = make(map[string]error)
	}
	configuration := config.Struct[lifecycleConfigID](func() lifecycleConfig { return lifecycleConfig{} }).Version("1").Build()
	descriptor := stream.MustDescriptor("fixture", lifecycleType.Identity(), timing.MustBase(1, 1000), property.New())
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", lifecycleType)})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", lifecycleType)}, []flow.Port{flow.Out("out", lifecycleType)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", lifecycleType)}, nil)

	source := plugin.NewComponent[lifecycleSourceID](
		plugin.Descriptor{DisplayName: "source", Version: "1"},
		configuration,
		plugin.WithSpec(plugin.Spec[lifecycleConfig, flow.Shape, stream.Descriptor]{
			Shape: plugin.StaticShape[lifecycleConfig](sourceShape),
			Compile: func(plugin.CompileContext, lifecycleConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
				return plugin.Compiled[flow.Shape, stream.Descriptor]{Plan: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor))}, nil
			},
			Open: func(ctx plugin.OpenContext, shape flow.Shape) (flow.Operator, error) {
				state.add("open/source")
				if err := state.failure("open/source"); err != nil {
					return nil, err
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
				if err := state.failure("open/processor"); err != nil {
					return nil, err
				}
				return &lifecycleProcessor{lifecycleBase: &lifecycleBase{shape: shape, name: "processor", state: state}}, nil
			},
			Finalizes: true,
		}),
		plugin.WithProcessor("in", lifecycleType, "out", lifecycleType),
	)
	sink := plugin.NewComponent[lifecycleSinkID](
		plugin.Descriptor{DisplayName: "sink", Version: "1"},
		configuration,
		plugin.WithSpec(plugin.Spec[lifecycleConfig, flow.Shape, stream.Descriptor]{
			Shape: plugin.StaticShape[lifecycleConfig](sinkShape),
			Compile: func(plugin.CompileContext, lifecycleConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
				return plugin.Compiled[flow.Shape, stream.Descriptor]{Plan: sinkShape, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
			},
			Open: func(_ plugin.OpenContext, shape flow.Shape) (flow.Operator, error) {
				state.add("open/sink")
				if err := state.failure("open/sink"); err != nil {
					return nil, err
				}
				return &lifecycleSink{lifecycleBase: &lifecycleBase{shape: shape, name: "sink", state: state}}, nil
			},
		}),
		plugin.WithWriter("in", lifecycleType),
	)
	definition := plugin.Define[lifecyclePluginID](plugin.Descriptor{DisplayName: "lifecycle", Version: "1"}, source, processor, sink)
	hostOptions := []Option{
		Plugins(plugin.NewSet(definition)),
		PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
		Observe(ObservationBasic),
	}
	hostOptions = append(hostOptions, options...)
	instance, err := New(hostOptions...)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("source", source.Identity(), config.NewPatch()),
			job.NewNode("processor", processor.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("processor", "in")),
			job.Connect(job.At("processor", "out"), job.At("sink", "in")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, graph)
	if err != nil {
		t.Fatal(err)
	}
	return instance, request
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
	result, err := prepared.Run(context.Background())
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
