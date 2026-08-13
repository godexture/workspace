package testkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/host"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type runnerKind uint8

const (
	componentRunner runnerKind = iota + 1
	formatRunner
	codecRunner
)

type fixturePluginID struct{}
type fixtureSourceID struct{}
type fixtureSinkID struct{}
type fixtureRejectID struct{}
type fixtureConfigID struct{}

type fixtureConfig struct{}

type fixturePlan struct{ shape flow.Shape }

type lifecycleState struct {
	sourceOpen  atomic.Int32
	sourceClose atomic.Int32
	sinkOpen    atomic.Int32
	sinkClose   atomic.Int32
	eof         atomic.Int32
}

type scenarioCore struct {
	host        *host.Host
	job         job.Job
	state       *lifecycleState
	selected    plugin.Identity
	purity      func(context.Context) (string, error)
	inspectPlan func(plan.Plan) error
	cancelCheck func() error
	finish      func() error
	cleanup     func() error
}

func (s *scenarioCore) close() error {
	if s == nil {
		return nil
	}
	if s.cleanup != nil {
		return s.cleanup()
	}
	return nil
}

func runCases[I, O any](t testing.TB, kind runnerKind, subject Subject[I, O], cases []Case[I, O]) {
	t.Helper()
	if !subject.valid() {
		t.Fatalf("testkit typed case subject is invalid")
	}
	if len(cases) == 0 {
		t.Fatalf("testkit typed case requires at least one case")
	}
	for index := range cases {
		current := cases[index]
		name := current.Name
		if name == "" {
			name = fmt.Sprintf("case-%d", index+1)
		}
		runNamed(t, name, func(child testing.TB) {
			runOne(child, kind, subject, current)
		})
	}
}

func runNamed(t testing.TB, name string, run func(testing.TB)) {
	t.Helper()
	if concrete, ok := t.(*testing.T); ok {
		concrete.Run(name, func(child *testing.T) { run(child) })
		return
	}
	run(t)
}

func runOne[I, O any](t testing.TB, kind runnerKind, subject Subject[I, O], test Case[I, O]) {
	t.Helper()
	if !test.Input.valid() {
		t.Fatalf("testkit typed case input fixture is invalid")
	}
	if !test.Want.valid() {
		t.Fatalf("testkit typed case expectation is invalid")
	}
	master := test.Input
	defer func() {
		if err := master.close(); err != nil {
			t.Errorf("testkit input ownership: %v", err)
		}
	}()
	if kind == formatRunner {
		if err := verifyFormatProbe(subject, master, test.Want.failure.stage != 0); err != nil {
			t.Fatalf("testkit Format probe: %v", err)
		}
	}

	factory := func() (*scenarioCore, error) {
		return newScenario(kind, subject, test.Config, master.clone(), test.Want.newRecorder())
	}
	executeCase(t, subject.identity, test.Want.failure, factory)
	if test.Want.failure.stage == 0 && acceptsRejection(kind, subject) {
		runRejected(t, kind, subject, test, master.clone())
	}
	subject.coverage.record(subject.identity)
}

func verifyFormatProbe[I, O any](subject Subject[I, O], input Fixture[I], expectFailure bool) error {
	component, ok := componentOf(subject.set, subject.identity)
	if !ok {
		return fmt.Errorf("subject %s is absent from its Set", subject.identity)
	}
	trait, ok := mediaformat.ReadOf(component)
	if !ok || !trait.HasProbe() {
		return nil
	}
	data, err := carrierBytes(input.values)
	if err != nil {
		return err
	}
	budget := job.DefaultBudget()
	views := make([]access.ProbeView, 0)
	seen := make(map[[2]int64]struct{})
	var bytes int64
	for round := 1; round <= budget.ProbeRounds; round++ {
		result, probeErr := trait.Probe(mediaformat.NewProbeContextAtEnd(context.Background(), views, int64(len(data))))
		if probeErr != nil {
			return probeErr
		}
		if result.Status() != mediaformat.ProbeNeedsData {
			if !expectFailure && result.Status() != mediaformat.ProbeMatch && result.Status() != mediaformat.ProbeFallback {
				return fmt.Errorf("successful fixture produced terminal probe status %v", result.Status())
			}
			return nil
		}
		added := false
		for _, request := range result.Needs() {
			key := [2]int64{request.Offset(), request.Length()}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			bytes += request.Length()
			if bytes > int64(budget.ProbeBytes) {
				return fmt.Errorf("probe requested %d bytes, budget is %d", bytes, budget.ProbeBytes)
			}
			start := request.Offset()
			end := request.End()
			if start > int64(len(data)) {
				start = int64(len(data))
			}
			if end > int64(len(data)) {
				end = int64(len(data))
			}
			if request.Offset() < int64(len(data)) {
				view, viewErr := access.NewProbeViewAt(request.Offset(), data[start:end])
				if viewErr != nil {
					return viewErr
				}
				views = append(views, view)
			}
			added = true
		}
		if !added {
			return errors.New("probe repeated cached ranges without reaching a terminal status")
		}
	}
	return fmt.Errorf("probe exceeded %d rounds", budget.ProbeRounds)
}

func executeCase(t testing.TB, identity plugin.Identity, failure failureExpectation, factory func() (*scenarioCore, error)) {
	t.Helper()
	if failure.stage == planFailure {
		first := planFailureScenario(t, factory, time.Hour, failure)
		second := planFailureScenario(t, factory, 2*time.Hour, failure)
		if first != second {
			t.Fatalf("planning failure purity: result changed with deadline: %q != %q", first, second)
		}
		return
	}
	first := planScenario(t, identity, factory, time.Hour)
	second := planScenario(t, identity, factory, 2*time.Hour)
	if first.fingerprint != second.fingerprint {
		t.Fatalf("Compile purity: planning result changed with deadline: %s != %s", first.fingerprint, second.fingerprint)
	}
	runCancelled(t, factory)
	runSuccessful(t, failure, factory, first.plan)
}

func planFailureScenario(t testing.TB, factory func() (*scenarioCore, error), timeout time.Duration, want failureExpectation) string {
	t.Helper()
	scenario, err := factory()
	if err != nil {
		t.Fatalf("testkit Plan failure scenario: %v", err)
	}
	defer func() {
		if err := scenario.close(); err != nil {
			t.Errorf("testkit Plan failure scenario cleanup: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, planErr := scenario.host.Plan(ctx, scenario.job)
	if planErr == nil {
		t.Fatalf("testkit Host.Plan succeeded, want %s", want.describe())
	}
	if !want.matches(planErr) {
		t.Fatalf("testkit Host.Plan error = %v, want %s", planErr, want.describe())
	}
	return planErr.Error()
}

type scenarioPlan struct {
	plan        plan.Plan
	fingerprint string
}

func planScenario(t testing.TB, identity plugin.Identity, factory func() (*scenarioCore, error), timeout time.Duration) scenarioPlan {
	t.Helper()
	scenario, err := factory()
	if err != nil {
		t.Fatalf("testkit Plan scenario: %v", err)
	}
	defer func() {
		if err := scenario.close(); err != nil {
			t.Errorf("testkit Plan scenario cleanup: %v", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	selected, err := scenario.host.Plan(ctx, scenario.job)
	if err != nil {
		t.Fatalf("testkit Host.Plan: %v", err)
	}
	selectedIdentity := identity
	if !scenario.selected.IsZero() {
		selectedIdentity = scenario.selected
	}
	assertSelectedSubject(t, selected, selectedIdentity)
	if scenario.inspectPlan != nil {
		if err := scenario.inspectPlan(selected); err != nil {
			t.Errorf("testkit Plan inspection: %v", err)
		}
	}
	fingerprint := selected.Fingerprint().String()
	if scenario.purity != nil {
		value, purityErr := scenario.purity(ctx)
		if purityErr != nil {
			t.Fatalf("testkit planning purity: %v", purityErr)
		}
		fingerprint += ":" + value
	}
	return scenarioPlan{plan: selected, fingerprint: fingerprint}
}

func runCancelled(t testing.TB, factory func() (*scenarioCore, error)) {
	t.Helper()
	scenario, err := factory()
	if err != nil {
		t.Fatalf("testkit cancellation scenario: %v", err)
	}
	defer func() {
		if err := scenario.close(); err != nil {
			t.Errorf("testkit cancellation cleanup: %v", err)
		}
	}()
	prepared, err := scenario.host.Prepare(context.Background(), scenario.job)
	if err != nil {
		t.Fatalf("testkit cancellation Prepare: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, runErr := prepared.Run(ctx)
	if !errors.Is(runErr, context.Canceled) {
		t.Errorf("testkit cancellation Run error = %v, want context.Canceled", runErr)
	}
	if len(result.Cleanup) != 0 {
		t.Errorf("testkit cancellation cleanup failures = %v", result.Cleanup)
	}
	if err := prepared.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("testkit cancellation Prepared.Close: %v", err)
	}
	if scenario.cancelCheck != nil {
		if err := scenario.cancelCheck(); err != nil {
			t.Errorf("testkit cancellation contract: %v", err)
		}
	}
}

func runSuccessful(t testing.TB, failure failureExpectation, factory func() (*scenarioCore, error), planned plan.Plan) {
	t.Helper()
	scenario, err := factory()
	if err != nil {
		t.Fatalf("testkit execution scenario: %v", err)
	}
	defer func() {
		if err := scenario.close(); err != nil {
			t.Errorf("testkit execution cleanup: %v", err)
		}
	}()
	prepared, err := scenario.host.Prepare(context.Background(), scenario.job)
	if err != nil {
		t.Fatalf("testkit Host.Prepare: %v", err)
	}
	if prepared.Plan().Fingerprint() != planned.Fingerprint() {
		t.Fatalf("Prepare selected %s, Plan selected %s", prepared.Plan().Fingerprint(), planned.Fingerprint())
	}
	if scenario.inspectPlan != nil {
		if err := scenario.inspectPlan(prepared.Plan()); err != nil {
			t.Errorf("testkit prepared Plan inspection: %v", err)
		}
	}
	result, runErr := prepared.Run(context.Background())
	closeErr := prepared.Close()
	if failure.stage == 0 {
		if runErr != nil || closeErr != nil || !result.Succeeded() {
			t.Fatalf("testkit Host.Run failed: run=%v close=%v result=%#v", runErr, closeErr, result)
		}
		if scenario.finish != nil {
			if err := scenario.finish(); err != nil {
				t.Errorf("testkit output: %v", err)
			}
		}
	} else {
		if runErr == nil {
			t.Errorf("testkit Host.Run succeeded, want %s", failure.describe())
		} else if !failure.matches(runErr) {
			t.Errorf("testkit Host.Run error = %v, diagnostics = %v, want %s", runErr, host.Diagnostics(runErr), failure.describe())
		}
		if len(result.Cleanup) != 0 {
			t.Errorf("testkit expected-failure cleanup = %v", result.Cleanup)
		}
	}
	assertLifecycle(t, scenario.state, failure.stage == 0)
}

func (f failureExpectation) matches(err error) bool {
	if f.target != nil {
		return errors.Is(err, f.target)
	}
	return f.code != "" && hasDiagnostic(err, f.code)
}

func (f failureExpectation) describe() string {
	if f.target != nil {
		return fmt.Sprintf("error matching %v", f.target)
	}
	return fmt.Sprintf("diagnostic %q", f.code)
}

func hasDiagnostic(err error, code string) bool {
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == code {
			return true
		}
	}
	return false
}

func assertSelectedSubject(t testing.TB, selected plan.Plan, identity plugin.Identity) {
	t.Helper()
	count := 0
	for _, node := range selected.Nodes() {
		if node.Component != identity.String() {
			continue
		}
		count++
		if node.Origin != plan.Requested {
			t.Errorf("testkit subject %s origin = %v, want requested", identity, node.Origin)
		}
	}
	if count != 1 {
		t.Fatalf("testkit selected subject %s %d times, want once", identity, count)
	}
}

func assertLifecycle(t testing.TB, state *lifecycleState, requireEOF bool) {
	t.Helper()
	if state == nil {
		t.Errorf("testkit fixture lifecycle state is absent")
		return
	}
	if state.sourceOpen.Load() != 1 || state.sourceClose.Load() != 1 || state.sinkOpen.Load() != 1 || state.sinkClose.Load() != 1 {
		t.Errorf("testkit fixture lifecycle source(open=%d close=%d) sink(open=%d close=%d), want one each",
			state.sourceOpen.Load(), state.sourceClose.Load(), state.sinkOpen.Load(), state.sinkClose.Load())
	}
	if requireEOF && state.eof.Load() == 0 {
		t.Errorf("testkit fixture source did not reach EOF")
	}
}

// scenarioOption adjusts how the testkit fixture pair behaves. Cases never see
// these; they exist so the runner can drive the same subject through its
// success and failure paths.
type scenarioOption func(*scenarioSettings)

type scenarioSettings struct{ reject *rejection }

func withRejection(value *rejection) scenarioOption {
	return func(settings *scenarioSettings) { settings.reject = value }
}

func newScenario[I, O any](kind runnerKind, subject Subject[I, O], patch config.Patch, input Fixture[I], output recorder[O], options ...scenarioOption) (*scenarioCore, error) {
	settings := scenarioSettings{}
	for _, option := range options {
		option(&settings)
	}
	state := &lifecycleState{}
	fixture := fixtureDefinition(kind, subject, &input, output, state, settings)
	set := subject.set.Add(fixture)
	instance, err := host.New(host.Plugins(set))
	if err != nil {
		return nil, err
	}
	request, closers, err := scenarioJob(kind, subject, set, patch, &input, settings)
	if err != nil {
		return nil, err
	}
	return &scenarioCore{
		host:   instance,
		job:    request,
		state:  state,
		finish: output.finish,
		cleanup: func() error {
			var problems []error
			problems = append(problems, input.close())
			for index := len(closers) - 1; index >= 0; index-- {
				problems = append(problems, closers[index].Close())
			}
			return errors.Join(problems...)
		},
	}, nil
}

func scenarioJob[I, O any](kind runnerKind, subject Subject[I, O], set plugin.Set, patch config.Patch, input *Fixture[I], settings scenarioSettings) (job.Job, []io.Closer, error) {
	source := job.NewNode("fixture-source", plugin.IdentityOf[fixtureSourceID](), config.NewPatch())
	target := job.NewNode("subject", subject.identity, patch)
	sink := job.NewNode("fixture-sink", plugin.IdentityOf[fixtureSinkID](), config.NewPatch())
	component, ok := componentOf(set, subject.identity)
	if !ok {
		return job.Job{}, nil, fmt.Errorf("subject %s is absent from its Set", subject.identity)
	}

	if kind == formatRunner {
		read, hasRead := mediaformat.ReadOf(component)
		write, hasWrite := mediaformat.WriteOf(component)
		if hasRead == hasWrite {
			return job.Job{}, nil, errors.New("Format subject must carry exactly one read or write trait")
		}
		if hasRead {
			return readFormatJob(subject, input, read, target, sink, settings)
		}
		return writeFormatJob(subject, input, write, source, target)
	}

	nodes := []job.Node{source, target, sink}
	edges := []job.Edge{job.Connect(job.At(source.ID(), "out"), job.At(target.ID(), subject.input.id))}
	nodes, edges = appendRejection(nodes, edges, subject.output.id, target, sink, "in", settings)
	graph, err := job.NewGraph(nodes, edges)
	if err != nil {
		return job.Job{}, nil, err
	}
	request, err := job.New(nil, nil, graph)
	return request, nil, err
}

// appendRejection routes the subject through the rejecting processor before
// its downstream consumer, so the failure reaches the subject's own Emit.
func appendRejection(nodes []job.Node, edges []job.Edge, outputPort string, target, consumer job.Node, consumerPort string, settings scenarioSettings) ([]job.Node, []job.Edge) {
	if settings.reject == nil {
		return nodes, append(edges, job.Connect(job.At(target.ID(), outputPort), job.At(consumer.ID(), consumerPort)))
	}
	reject := job.NewNode("fixture-reject", plugin.IdentityOf[fixtureRejectID](), config.NewPatch())
	return append(nodes, reject), append(edges,
		job.Connect(job.At(target.ID(), outputPort), job.At(reject.ID(), "in")),
		job.Connect(job.At(reject.ID(), "out"), job.At(consumer.ID(), consumerPort)),
	)
}

func readFormatJob[I, O any](subject Subject[I, O], input *Fixture[I], trait mediaformat.ReadTrait, target, sink job.Node, settings scenarioSettings) (job.Job, []io.Closer, error) {
	if subject.input.schema.Identity() != access.Bytes().Identity() {
		return job.Job{}, nil, errors.New("read Format input schema must be access.Bytes")
	}
	_ = trait
	_ = input
	reference, err := access.NewReference("testkit", "input")
	if err != nil {
		return job.Job{}, nil, err
	}
	boundary, err := job.InputFromReference(reference)
	if err != nil {
		return job.Job{}, nil, err
	}
	nodes, edges := appendRejection([]job.Node{target, sink}, nil, subject.output.id, target, sink, "in", settings)
	graph, err := job.NewGraph(nodes, edges)
	if err != nil {
		return job.Job{}, nil, err
	}
	request, err := job.New([]job.Input{boundary}, nil, graph)
	return request, nil, err
}

func writeFormatJob[I, O any](subject Subject[I, O], input *Fixture[I], trait mediaformat.WriteTrait, source, target job.Node) (job.Job, []io.Closer, error) {
	if subject.output.schema.Identity() != access.Writes().Identity() {
		return job.Job{}, nil, errors.New("write Format output schema must be access.Writes")
	}
	_ = trait
	_ = input
	reference, err := access.NewReference("testkit", "output")
	if err != nil {
		return job.Job{}, nil, err
	}
	boundary, err := job.OutputToReference(reference)
	if err != nil {
		return job.Job{}, nil, err
	}
	graph, err := job.NewGraph(
		[]job.Node{source, target},
		[]job.Edge{job.Connect(job.At(source.ID(), "out"), job.At(target.ID(), subject.input.id))},
	)
	if err != nil {
		return job.Job{}, nil, err
	}
	request, err := job.New(nil, []job.Output{boundary}, graph)
	return request, nil, err
}

func carrierBytes[T any](values []T) ([]byte, error) {
	var result []byte
	for _, value := range values {
		handle, ok := any(value).(buffer.Handle)
		if !ok || !handle.Valid() {
			return nil, errors.New("read Format fixture contains a non-byte payload")
		}
		result = handle.Bytes().AppendTo(result)
	}
	return result, nil
}

func componentOf(set plugin.Set, identity plugin.Identity) (plugin.Component, bool) {
	for _, component := range set.Components() {
		if component.Identity() == identity {
			return component, true
		}
	}
	return plugin.Component{}, false
}

func fixtureDefinition[I, O any](kind runnerKind, subject Subject[I, O], input *Fixture[I], output recorder[O], state *lifecycleState, settings scenarioSettings) plugin.Definition {
	schema := config.Struct[fixtureConfigID](func() fixtureConfig { return fixtureConfig{} }).Version("1").Build()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", subject.input.schema)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", subject.output.schema)}, nil)
	sourceSpec := plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](sourceShape),
		Compile: func(plugin.CompileContext, fixtureConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			return plugin.Compiled[fixturePlan, stream.Descriptor]{
				Plan:    fixturePlan{shape: sourceShape.Clone()},
				Outputs: flow.NewDescriptors(flow.Describe("out", input.descriptor)),
			}, nil
		},
		Open: func(plugin.OpenContext, fixturePlan) (flow.Operator, error) {
			state.sourceOpen.Add(1)
			return &fixtureReader[I]{shape: sourceShape, input: input, state: state}, nil
		},
	}
	sinkSpec := plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](sinkShape),
		Compile: func(_ plugin.CompileContext, _ fixtureConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			if _, ok := inputs.One("in"); !ok {
				return plugin.Compiled[fixturePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("testkit.output")),
				}}, nil
			}
			return plugin.Compiled[fixturePlan, stream.Descriptor]{Plan: fixturePlan{shape: sinkShape.Clone()}}, nil
		},
		Open: func(plugin.OpenContext, fixturePlan) (flow.Operator, error) {
			state.sinkOpen.Add(1)
			return &fixtureWriter[O]{shape: sinkShape, output: output, state: state}, nil
		},
	}
	components := []plugin.Component{}
	if settings.reject != nil {
		components = append(components, rejectComponent(subject, settings.reject))
	}
	sourceOptions := []plugin.ComponentOption{plugin.WithSpec(sourceSpec), plugin.WithReader("out", subject.input.schema)}
	sinkOptions := []plugin.ComponentOption{plugin.WithSpec(sinkSpec), plugin.WithWriter("in", subject.output.schema)}
	if kind == formatRunner {
		if component, ok := componentOf(subject.set, subject.identity); ok {
			if _, read := mediaformat.ReadOf(component); read {
				capabilities, _ := access.NewCapabilities(access.SequentialRead, access.RandomRead, access.StableSize)
				sourceOptions = append(sourceOptions, access.Source("testkit", capabilities, func(context.Context, access.Reference, access.Selection) (access.Session, error) {
					payload, err := carrierBytes(input.values)
					if err != nil {
						return nil, err
					}
					return &readSession{data: payload, caps: capabilities}, nil
				}))
			}
			if _, write := mediaformat.WriteOf(component); write {
				capabilities, _ := access.NewCapabilities(access.SequentialWrite, access.RandomWrite)
				sinkOptions = append(sinkOptions, access.Sink("testkit", capabilities, access.LiveNoCommit, func(context.Context, access.Reference, access.Selection) (access.Session, error) {
					return &writeSession{caps: capabilities}, nil
				}))
			}
		}
	}
	source := plugin.NewComponent[fixtureSourceID](plugin.Descriptor{DisplayName: "testkit source"}, schema, sourceOptions...)
	sink := plugin.NewComponent[fixtureSinkID](plugin.Descriptor{DisplayName: "testkit sink"}, schema, sinkOptions...)
	components = append(components, source, sink)
	return plugin.Define[fixturePluginID](plugin.Descriptor{DisplayName: "testkit fixtures", Version: "1"}, components...)
}

// rejectComponent is a pass-through processor that refuses one item. Host
// fuses adjacent linear processors into a single island, so the subject sees
// its Emit fail inside its own Process call.
func rejectComponent[I, O any](subject Subject[I, O], reject *rejection) plugin.Component {
	schema := config.Struct[fixtureConfigID](func() fixtureConfig { return fixtureConfig{} }).Version("1").Build()
	shape := flow.NewShape([]flow.Port{flow.In("in", subject.output.schema)}, []flow.Port{flow.Out("out", subject.output.schema)})
	spec := plugin.Spec[fixtureConfig, fixturePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[fixtureConfig](shape),
		Compile: func(_ plugin.CompileContext, _ fixtureConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[fixturePlan, stream.Descriptor], error) {
			input, ok := inputs.One("in")
			if !ok {
				return plugin.Compiled[fixturePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{
					plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("testkit.reject")),
				}}, nil
			}
			return plugin.Compiled[fixturePlan, stream.Descriptor]{
				Plan:    fixturePlan{shape: shape.Clone()},
				Outputs: flow.NewDescriptors(flow.Describe("out", input)),
			}, nil
		},
		Open: func(plugin.OpenContext, fixturePlan) (flow.Operator, error) {
			return &rejectOperator[O]{shape: shape.Clone(), reject: reject}, nil
		},
	}
	return plugin.NewComponent[fixtureRejectID](plugin.Descriptor{DisplayName: "testkit rejection"}, schema,
		plugin.WithSpec(spec), plugin.WithProcessor("in", subject.output.schema, "out", subject.output.schema))
}

type rejectOperator[T any] struct {
	shape  flow.Shape
	reject *rejection
}

func (o *rejectOperator[T]) Ports() flow.Shape { return o.shape.Clone() }
func (*rejectOperator[T]) Close() error        { return nil }

func (o *rejectOperator[T]) Process(ctx context.Context, input *flow.Item[T], output flow.Emitter[T]) error {
	if err := o.reject.accept(); err != nil {
		return err
	}
	return output.Emit(ctx, input)
}

func (*rejectOperator[T]) Flush(context.Context, flow.Emitter[T]) error { return nil }

type fixtureReader[T any] struct {
	shape  flow.Shape
	input  *Fixture[T]
	state  *lifecycleState
	mu     sync.Mutex
	index  int
	closed bool
}

func (r *fixtureReader[T]) Ports() flow.Shape { return r.shape.Clone() }

func (r *fixtureReader[T]) Read(ctx context.Context, into *flow.Item[T]) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.index >= len(r.input.values) {
		r.state.eof.Add(1)
		return io.EOF
	}
	*into = r.input.emit(r.index)
	r.index++
	return nil
}

func (r *fixtureReader[T]) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		r.state.sourceClose.Add(1)
	}
	return nil
}

type fixtureWriter[T any] struct {
	shape  flow.Shape
	output recorder[T]
	state  *lifecycleState
	closed bool
}

func (w *fixtureWriter[T]) Ports() flow.Shape { return w.shape.Clone() }

func (w *fixtureWriter[T]) Write(_ context.Context, input *flow.Item[T]) error {
	defer input.Drop()
	if !input.Valid() {
		return errors.New("testkit sink received an invalid input")
	}
	w.output.accept(input.Value())
	return nil
}

func (w *fixtureWriter[T]) Close() error {
	if !w.closed {
		w.closed = true
		w.state.sinkClose.Add(1)
	}
	return nil
}

type readSession struct {
	data   []byte
	offset int64
	closed atomic.Bool
	caps   access.Capabilities
}

func (s *readSession) Capabilities() access.Capabilities { return s.caps }
func (s *readSession) Close() error {
	s.closed.Store(true)
	return nil
}
func (s *readSession) Read(ctx context.Context, destination []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	count, err := s.ReadAt(ctx, destination, s.offset)
	s.offset += int64(count)
	return count, err
}
func (s *readSession) ReadAt(ctx context.Context, destination []byte, offset int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if offset < 0 || offset >= int64(len(s.data)) {
		return 0, io.EOF
	}
	count := copy(destination, s.data[offset:])
	if count < len(destination) {
		return count, io.EOF
	}
	return count, nil
}

func (s *readSession) Size(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s.closed.Load() {
		return 0, errors.New("testkit read session is closed")
	}
	return int64(len(s.data)), nil
}

type writeSession struct{ caps access.Capabilities }

func (s *writeSession) Capabilities() access.Capabilities { return s.caps }
func (*writeSession) Close() error                        { return nil }
func (*writeSession) Write(ctx context.Context, value []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return len(value), nil
}
func (*writeSession) WriteAt(ctx context.Context, value []byte, _ int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return len(value), nil
}
