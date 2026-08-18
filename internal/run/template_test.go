package run

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/cancel"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/observe"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/internal/task"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
)

type templateOperator struct{ shape flow.Shape }

func (o templateOperator) Ports() flow.Shape { return o.shape.Clone() }
func (templateOperator) Close() error        { return nil }

type templateReader struct {
	templateOperator
	typ    schema.Type[int]
	values []int
	index  int
}

func (r *templateReader) Read(_ context.Context, into *flow.Item[int]) error {
	if r.index == len(r.values) {
		return io.EOF
	}
	typ := r.typ
	if !typ.Valid() {
		typ = templateInput
	}
	into.Set(r.values[r.index])
	r.index++
	return nil
}

type panickingProcessor struct{ templateOperator }

func (*panickingProcessor) Process(context.Context, *flow.Item[int], flow.Emitter[int]) error {
	panic("processor panic")
}

func (*panickingProcessor) Flush(context.Context, flow.Emitter[int]) error { return nil }

type failingWriter struct {
	templateOperator
	failAt int
	writes int
}

func (w *failingWriter) Write(_ context.Context, input *flow.Item[int]) error {
	if w.writes == w.failAt {
		return errors.New("sink failure")
	}
	w.writes++
	input.Drop()
	return nil
}

type templateProcessor struct {
	templateOperator
	in  schema.Type[int]
	out schema.Type[int]
	add int
}

func (p *templateProcessor) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	item := flow.NewItem(input.Value()+p.add, p.out, &testDomain)
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	input.Drop()
	return nil
}

func (*templateProcessor) Flush(context.Context, flow.Emitter[int]) error { return nil }

type templateWriter struct {
	templateOperator
	mu     sync.Mutex
	values []int
}

func (w *templateWriter) Write(_ context.Context, input *flow.Item[int]) error {
	w.mu.Lock()
	w.values = append(w.values, input.Value())
	w.mu.Unlock()
	input.Drop()
	return nil
}

func (w *templateWriter) Values() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]int(nil), w.values...)
}

type (
	templateInputID  struct{}
	templateMiddleID struct{}
	templateOutputID struct{}
)

var (
	templateInput  = schema.Define[templateInputID](schema.Traits[int]{})
	templateMiddle = schema.Define[templateMiddleID](schema.Traits[int]{})
	templateOutput = schema.Define[templateOutputID](schema.Traits[int]{})
	templateQueue  = job.QueuePolicy{Items: 4}
)

func compileFixture(values []Node, edges []job.Edge, policy job.QueuePolicy, alignment job.AlignmentPolicy) (Template, error) {
	return Compile(describeFixtureNodes(values, edges), edges, policy, alignment)
}

func describeFixtureNodes(values []Node, edges []job.Edge) []Node {
	result := append([]Node(nil), values...)
	byID := make(map[job.NodeID]int, len(result))
	for index, value := range result {
		byID[value.ID] = index
	}
	for _, edge := range edges {
		index := byID[edge.From().Node()]
		if len(result[index].Outputs.At(edge.From().ID())) != 0 {
			continue
		}
		for _, port := range result[index].Shape.Outputs {
			if port.ID() != edge.From().ID() {
				continue
			}
			base := timing.Base{}
			if port.Schema().HasTime() {
				base = timing.MustBase(1, 1_000)
			}
			descriptor := stream.MustDescriptor(stream.ID(edge.From().String()), port.Schema(), base, property.New())
			result[index].Outputs = flow.NewDescriptors(append(result[index].Outputs.Bindings(), flow.Describe(edge.From().ID(), descriptor))...)
			break
		}
	}
	byTarget := make(map[job.NodeID][]job.Edge, len(result))
	for _, edge := range edges {
		byTarget[edge.To().Node()] = append(byTarget[edge.To().Node()], edge)
	}
	for id, incoming := range byTarget {
		sort.Slice(incoming, func(left, right int) bool {
			if incoming[left].To().ID() != incoming[right].To().ID() {
				return incoming[left].To().ID() < incoming[right].To().ID()
			}
			return incoming[left].From().String() < incoming[right].From().String()
		})
		index := byID[id]
		bindings := result[index].Inputs.Bindings()
		for _, edge := range incoming {
			if len(result[index].Inputs.At(edge.To().ID())) != 0 {
				continue
			}
			from := result[byID[edge.From().Node()]]
			for _, descriptor := range from.Outputs.At(edge.From().ID()) {
				bindings = append(bindings, flow.Describe(edge.To().ID(), descriptor))
			}
		}
		result[index].Inputs = flow.NewDescriptors(bindings...)
	}
	return result
}

// island drives one execution the way Host does, over one ledger, and reads
// the ledger afterwards. Everything a run produced is there, so a test asks the
// ledger rather than collecting failures from several return values.
type island struct {
	ledger    *journal.Ledger
	execution *Execution
	report    task.Report
}

func buildIsland(t testing.TB, template Template, operators ...flow.Operator) *island {
	t.Helper()
	ledger := journal.NewLedger()
	execution, err := template.Build(ledger, operators)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return &island{ledger: ledger, execution: execution}
}

func (i *island) run(ctx context.Context) *island {
	phase, phaseCancel, detach := cancel.Link(ctx)
	defer detach()
	defer phaseCancel(context.Canceled)
	job, jobCancel := context.WithCancelCause(phase)
	stop := func(cause error) {
		if cause == nil {
			cause = context.Canceled
		}
		if journal.OperationOf(cause) != journal.Flush {
			phaseCancel(cause)
		}
		jobCancel(cause)
	}
	group := task.NewLinked(job, i.ledger, stop)
	if err := i.execution.Start(group); err != nil {
		i.ledger.Record(journal.Entry{Kind: journal.WorkError, Operation: journal.Run, Task: "runtime/start", Err: err})
		return i
	}
	if err := i.execution.WaitSources(job, group); err != nil {
		i.ledger.Record(journal.Entry{Kind: journal.WorkError, Operation: journal.Run, Task: "runtime/source", Err: err})
		stop(err)
		i.execution.Abort()
	} else if err := i.execution.Quiesce(group.Context()); err != nil {
		i.ledger.Record(journal.Entry{Kind: journal.WorkError, Operation: journal.Run, Task: "runtime/quiesce", Err: err})
		stop(err)
		i.execution.Abort()
	} else {
		// Flushing is the run's own lifecycle step from here, the same way Host
		// marks it, so a component releasing something it retained lands under
		// Flush even with no runtime span open for it.
		i.ledger.EnterStage(journal.Flush)
		if err := i.execution.Finish(phase); err != nil {
			i.ledger.Record(journal.Entry{Kind: journal.WorkError, Operation: journal.Flush, Task: "runtime/finish", Err: err})
			stop(err)
		}
	}
	i.report = group.Wait(context.Background())
	i.ledger.EnterStage(journal.Discard)
	i.execution.Discard()
	return i
}

func runIsland(t testing.TB, ctx context.Context, template Template, operators ...flow.Operator) *island {
	t.Helper()
	return buildIsland(t, template, operators...).run(ctx)
}

func (i *island) events() []journal.Failure { return i.ledger.Events() }

func (i *island) failures() []journal.Failure { return selectFailures(i.ledger, false) }

func (i *island) cleanups() []journal.Failure { return selectFailures(i.ledger, true) }

func selectFailures(ledger *journal.Ledger, cleanup bool) []journal.Failure {
	var result []journal.Failure
	for _, event := range ledger.Events() {
		if event.Kind.Cleanup() == cleanup {
			result = append(result, event)
		}
	}
	return result
}

func (i *island) succeeded() bool { return i.report.Complete() && len(i.events()) == 0 }

func TestCompileFusesMaximalLinearProcessorIsland(t *testing.T) {
	nodes := []Node{
		{ID: "source", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)}), Execution: drive.NewSource("out", templateInput)},
		{ID: "first", Shape: flow.NewShape([]flow.Port{flow.In("in", templateInput)}, []flow.Port{flow.Out("out", templateMiddle)}), Execution: drive.NewProcessor("in", templateInput, "out", templateMiddle)},
		{ID: "second", Shape: flow.NewShape([]flow.Port{flow.In("in", templateMiddle)}, []flow.Port{flow.Out("out", templateOutput)}), Execution: drive.NewProcessor("in", templateMiddle, "out", templateOutput)},
		{ID: "sink", Shape: flow.NewShape([]flow.Port{flow.In("in", templateOutput)}, nil), Execution: drive.NewSink("in", templateOutput)},
	}
	edges := []job.Edge{
		job.Connect(job.At("source", "out"), job.At("first", "in")),
		job.Connect(job.At("first", "out"), job.At("second", "in")),
		job.Connect(job.At("second", "out"), job.At("sink", "in")),
	}
	template, err := compileFixture(nodes, edges, templateQueue, job.AlignmentPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	runtime := template.Projection()
	if !template.Executable() || len(runtime.Islands) != 3 || len(runtime.Islands[1].Nodes) != 2 || runtime.Islands[1].Nodes[0] != "first" || runtime.Islands[1].Nodes[1] != "second" {
		t.Fatalf("islands = %#v", runtime.Islands)
	}
	if len(runtime.Buffers) != 2 || !runtime.Buffers[0].Reason.Has(plan.SourceBuffer) || !runtime.Buffers[1].Reason.Has(plan.SinkBuffer) {
		t.Fatalf("buffers = %#v", runtime.Buffers)
	}
}

func TestCompileProjectsFanoutAndCanonicalZip(t *testing.T) {
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", templateInput, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", templateOutput)},
	)
	nodes := []Node{
		{ID: "a", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)}), Execution: drive.NewSource("out", templateInput)},
		{ID: "b", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)}), Execution: drive.NewSource("out", templateInput)},
		{ID: "join", Shape: joinShape, Execution: drive.NewJoiner("in", templateInput, flow.ZipFanIn, "out", templateOutput)},
		{ID: "sink", Shape: flow.NewShape([]flow.Port{flow.In("in", templateOutput)}, nil), Execution: drive.NewSink("in", templateOutput)},
	}
	edges := []job.Edge{
		job.Connect(job.At("b", "out"), job.At("join", "in")),
		job.Connect(job.At("a", "out"), job.At("join", "in")),
		job.Connect(job.At("join", "out"), job.At("sink", "in")),
	}
	template, err := compileFixture(nodes, edges, templateQueue, job.AlignmentPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	runtime := template.Projection()
	if len(runtime.FanIns) != 1 || runtime.FanIns[0].Node != "join" || runtime.FanIns[0].Port != "in" || runtime.FanIns[0].Policy != flow.ZipFanIn {
		t.Fatalf("fan-in = %#v", runtime.FanIns)
	}
	if len(runtime.Buffers) != 3 || !runtime.Buffers[0].Reason.Has(plan.FanInBuffer) || !runtime.Buffers[1].Reason.Has(plan.FanInBuffer) {
		t.Fatalf("fan-in buffers = %#v", runtime.Buffers)
	}
}

func TestCompileRejectsOwnedFanOutWithoutFork(t *testing.T) {
	type ownedID struct{}
	typ := schema.Define[ownedID](schema.Traits[int]{Drop: func(int) {}})
	nodes := []Node{
		{ID: "source", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", typ)}), Execution: drive.NewSource("out", typ)},
		{ID: "left", Shape: flow.NewShape([]flow.Port{flow.In("in", typ)}, nil), Execution: drive.NewSink("in", typ)},
		{ID: "right", Shape: flow.NewShape([]flow.Port{flow.In("in", typ)}, nil), Execution: drive.NewSink("in", typ)},
	}
	edges := []job.Edge{
		job.Connect(job.At("source", "out"), job.At("left", "in")),
		job.Connect(job.At("source", "out"), job.At("right", "in")),
	}
	_, err := compileFixture(nodes, edges, templateQueue, job.AlignmentPolicy{})
	if !errors.Is(err, drive.ErrForkTrait) || !errors.Is(err, ErrTopology) {
		t.Fatalf("owned fan-out Compile error = %v", err)
	}
}

func TestCompileAllowsUnownedOneFanOut(t *testing.T) {
	type valueID struct{}
	typ := schema.Define[valueID](schema.Traits[int]{})
	nodes := []Node{
		{ID: "source", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", typ)}), Execution: drive.NewSource("out", typ)},
		{ID: "left", Shape: flow.NewShape([]flow.Port{flow.In("in", typ)}, nil), Execution: drive.NewSink("in", typ)},
		{ID: "right", Shape: flow.NewShape([]flow.Port{flow.In("in", typ)}, nil), Execution: drive.NewSink("in", typ)},
	}
	edges := []job.Edge{
		job.Connect(job.At("source", "out"), job.At("left", "in")),
		job.Connect(job.At("source", "out"), job.At("right", "in")),
	}
	if _, err := compileFixture(nodes, edges, templateQueue, job.AlignmentPolicy{}); err != nil {
		t.Fatalf("unowned One fan-out Compile = %v", err)
	}
}

func TestCompileRejectsRoutedSourceFanOutWithoutFork(t *testing.T) {
	type ownedRouteID struct{}
	typ := schema.Define[ownedRouteID](schema.Traits[int]{Drop: func(int) {}})
	nodes := []Node{
		{ID: "source", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", typ, flow.Many())}), Execution: drive.NewRoutedSource("out", typ)},
		{ID: "left", Shape: flow.NewShape([]flow.Port{flow.In("in", typ)}, nil), Execution: drive.NewSink("in", typ)},
		{ID: "right", Shape: flow.NewShape([]flow.Port{flow.In("in", typ)}, nil), Execution: drive.NewSink("in", typ)},
	}
	edges := []job.Edge{
		job.Connect(job.At("source", "out"), job.At("left", "in")),
		job.Connect(job.At("source", "out"), job.At("right", "in")),
	}
	_, err := compileFixture(nodes, edges, templateQueue, job.AlignmentPolicy{})
	if !errors.Is(err, drive.ErrForkTrait) || !errors.Is(err, ErrTopology) {
		t.Fatalf("owned routed-source fan-out Compile error = %v", err)
	}
}

func TestCompileSelectsTraitAwareQueueLimitsAndFanInTolerance(t *testing.T) {
	type timedID struct{}
	typ := schema.Define[timedID](schema.Traits[int]{
		Size: func(int) int { return 8 },
		Time: func(value int) (int64, bool) { return int64(value), true },
	})
	base := timing.MustBase(1, 1_000)
	descriptor := stream.MustDescriptor("timed", typ.Descriptor(), base, property.New())
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", typ)},
	)
	nodes := []Node{
		{ID: "a", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", typ)}), Outputs: flow.NewDescriptors(flow.Describe("out", descriptor)), Execution: drive.NewSource("out", typ)},
		{ID: "b", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", typ)}), Outputs: flow.NewDescriptors(flow.Describe("out", descriptor)), Execution: drive.NewSource("out", typ)},
		{ID: "join", Shape: joinShape, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor)), Execution: drive.NewJoiner("in", typ, flow.ZipFanIn, "out", typ)},
		{ID: "sink", Shape: flow.NewShape([]flow.Port{flow.In("in", typ)}, nil), Execution: drive.NewSink("in", typ)},
	}
	edges := []job.Edge{
		job.Connect(job.At("a", "out"), job.At("join", "in")),
		job.Connect(job.At("b", "out"), job.At("join", "in")),
		job.Connect(job.At("join", "out"), job.At("sink", "in")),
	}
	template, err := compileFixture(nodes, edges, job.QueuePolicy{Items: 2, Bytes: 1 << 20, Span: 250 * time.Millisecond}, job.AlignmentPolicy{Zip: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	runtime := template.Projection()
	if len(runtime.FanIns) != 1 || runtime.FanIns[0].Tolerance != 250*time.Millisecond {
		t.Fatalf("fan-in tolerance = %#v", runtime.FanIns)
	}
	for _, buffer := range runtime.Buffers {
		if buffer.Limit != (plan.Limit{Items: 2, Bytes: 1 << 20, Span: 250 * time.Millisecond}) {
			t.Fatalf("buffer limit = %#v", buffer.Limit)
		}
	}
	template, err = compileFixture(nodes, edges, job.QueuePolicy{Items: 2, Bytes: 1 << 20}, job.AlignmentPolicy{Zip: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	runtime = template.Projection()
	if len(runtime.FanIns) != 1 || runtime.FanIns[0].Tolerance != 250*time.Millisecond {
		t.Fatalf("fan-in tolerance without queue span = %#v", runtime.FanIns)
	}
	for _, buffer := range runtime.Buffers {
		if buffer.Limit != (plan.Limit{Items: 2, Bytes: 1 << 20}) {
			t.Fatalf("buffer limit without queue span = %#v", buffer.Limit)
		}
	}
	template, err = compileFixture(nodes, edges, job.QueuePolicy{Items: 2, Bytes: 1 << 20, Span: 250 * time.Millisecond}, job.AlignmentPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	runtime = template.Projection()
	if len(runtime.FanIns) != 1 || runtime.FanIns[0].Tolerance != 0 {
		t.Fatalf("fan-in tolerance without alignment policy = %#v", runtime.FanIns)
	}
}

func TestCompileRejectsNodesOutsideTopologicalOrder(t *testing.T) {
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", templateInput)}, nil)
	_, err := compileFixture([]Node{
		{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", templateInput)},
		{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", templateInput)},
	}, []job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))}, templateQueue, job.AlignmentPolicy{})
	if !errors.Is(err, ErrTopologyOrder) {
		t.Fatalf("topological order error = %v", err)
	}
}

func TestCompileRejectsNegativeAlignmentTolerance(t *testing.T) {
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", templateInput)}, nil)
	nodes := []Node{
		{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", templateInput)},
		{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", templateInput)},
	}
	edges := []job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))}
	if _, err := compileFixture(nodes, edges, templateQueue, job.AlignmentPolicy{}); err != nil {
		t.Fatalf("valid alignment policy rejected valid graph: %v", err)
	}
	_, err := compileFixture(
		nodes,
		edges,
		templateQueue,
		job.AlignmentPolicy{Zip: -time.Nanosecond},
	)
	if !errors.Is(err, ErrTopology) {
		t.Fatalf("negative alignment tolerance error = %v", err)
	}
}

func TestCompileKeepsPlanningOnlyGraphNonExecutable(t *testing.T) {
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})},
			{ID: "sink", Shape: flow.NewShape([]flow.Port{flow.In("in", templateInput)}, nil)},
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if template.Executable() || template.Projection().Executable {
		t.Fatalf("planning-only template = %#v", template.Projection())
	}
}

func TestBuildRunsSourceAndBoundaryTasksAroundFusedProcessors(t *testing.T) {
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})
	firstShape := flow.NewShape([]flow.Port{flow.In("in", templateInput)}, []flow.Port{flow.Out("out", templateMiddle)})
	secondShape := flow.NewShape([]flow.Port{flow.In("in", templateMiddle)}, []flow.Port{flow.Out("out", templateOutput)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", templateOutput)}, nil)
	nodes := []Node{
		{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", templateInput)},
		{ID: "first", Shape: firstShape, Execution: drive.NewProcessor("in", templateInput, "out", templateMiddle)},
		{ID: "second", Shape: secondShape, Execution: drive.NewProcessor("in", templateMiddle, "out", templateOutput)},
		{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", templateOutput)},
	}
	edges := []job.Edge{
		job.Connect(job.At("source", "out"), job.At("first", "in")),
		job.Connect(job.At("first", "out"), job.At("second", "in")),
		job.Connect(job.At("second", "out"), job.At("sink", "in")),
	}
	template, err := compileFixture(nodes, edges, templateQueue, job.AlignmentPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	writer := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	value := buildIsland(t, template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, values: []int{1, 2, 3}},
		&templateProcessor{templateOperator: templateOperator{shape: firstShape}, in: templateInput, out: templateMiddle, add: 10},
		&templateProcessor{templateOperator: templateOperator{shape: secondShape}, in: templateMiddle, out: templateOutput, add: 100},
		writer,
	)
	sources, edgeTasks := value.execution.TaskCounts()
	if sources != 1 || edgeTasks != 2 {
		t.Fatalf("task counts = sources %d edges %d", sources, edgeTasks)
	}
	if value.run(context.Background()); !value.succeeded() {
		t.Fatalf("run = %#v, ledger = %#v", value.report, value.events())
	}
	if got := writer.Values(); len(got) != 3 || got[0] != 111 || got[1] != 112 || got[2] != 113 {
		t.Fatalf("sink values = %v", got)
	}
}

func TestTaskTopPanicIdentifiesNodeAndDropsActiveItem(t *testing.T) {
	type panicSchemaID struct{}
	var drops atomic.Int32
	typ := schema.Define[panicSchemaID](schema.Traits[int]{Drop: func(int) { drops.Add(1) }})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "panic", Shape: processorShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("panic", "in")),
			job.Connect(job.At("panic", "out"), job.At("sink", "in")),
		},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	value := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}},
		&panickingProcessor{templateOperator{shape: processorShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	var panicErr *journal.PanicError
	if recorded := value.failures(); len(recorded) != 1 || !errors.As(recorded[0].Err, &panicErr) || panicErr.Location != "panic" {
		t.Fatalf("panic report = %#v", value.events())
	}
	if drops.Load() != 1 {
		t.Fatalf("panic drop count = %d", drops.Load())
	}
}

func TestFailureDropsEveryItemAcceptedFromSource(t *testing.T) {
	type failureSchemaID struct{}
	const failAt = 50_000
	for run := range 40 {
		var dropped atomic.Int64
		typ := schema.Define[failureSchemaID](schema.Traits[int]{Drop: func(int) { dropped.Add(1) }})
		sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
		sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
		template, err := compileFixture(
			[]Node{
				{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
				{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
			},
			[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
			templateQueue,
			job.AlignmentPolicy{},
		)
		if err != nil {
			t.Fatal(err)
		}
		reader := &templateReader{
			templateOperator: templateOperator{shape: sourceShape},
			typ:              typ,
			values:           make([]int, failAt+templateQueue.Items*2),
		}
		value := runIsland(t, context.Background(), template,
			reader,
			&failingWriter{templateOperator: templateOperator{shape: sinkShape}, failAt: failAt},
		)
		if len(value.events()) == 0 {
			t.Fatalf("run %d unexpectedly succeeded", run)
		}
		emitted := int64(reader.index)
		if got := dropped.Load(); got != emitted {
			t.Fatalf("run %d dropped = %d, want %d emitted items", run, got, emitted)
		}
	}
}

type panickingWriter struct{ templateOperator }

func (panickingWriter) Write(context.Context, *flow.Item[int]) error { panic("writer panicked") }

// A panic discards the value its task was returning, and the cleanup that ran
// during the unwind had just joined its failures into exactly that value. The
// scope carries them to the boundary instead, so a release that failed beside a
// panic is still reported -- once, because a task that returns normally carries
// them out itself.
func TestCleanupFailuresSurviveThePanicBesideThem(t *testing.T) {
	type cleanupSchemaID struct{}
	for _, test := range []struct {
		name     string
		writer   func(flow.Shape) flow.Operator
		panicked bool
	}{
		{
			name:     "the consumer panics",
			writer:   func(shape flow.Shape) flow.Operator { return panickingWriter{templateOperator{shape: shape}} },
			panicked: true,
		},
		{
			name: "the consumer reports a failure",
			writer: func(shape flow.Shape) flow.Operator {
				return &failingWriter{templateOperator: templateOperator{shape: shape}}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Only the values left in the ring fail to release, so the drain is
			// the one thing that fails besides the consumer.
			typ := schema.Define[cleanupSchemaID](schema.Traits[int]{
				Drop: func(value int) {
					if value == 0 {
						panic("declared drop panicked")
					}
				},
			})
			sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
			sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
			sinkLink, err := drive.NewSink("in", typ).OpenSink(test.writer(sinkShape))
			if err != nil {
				t.Fatal(err)
			}
			ledger := journal.NewLedger()
			buffered, bufferTask, err := drive.NewSource("out", typ).Buffer(queue.Limit{Items: 4}, sinkLink, ledger.Domain("buffer", "edge"))
			if err != nil {
				t.Fatal(err)
			}
			reader := &templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 0, 0}}
			sourceTask, err := drive.NewSource("out", typ).OpenSource(reader, buffered, ledger.Domain("source", "source"))
			if err != nil {
				t.Fatal(err)
			}
			// Fill and seal the edge before its drain starts. Every owner the
			// consumer can leave behind is therefore causally accepted before
			// its failure cancels the group.
			if err := sourceTask.Domain().Perform(journal.Run, func(span *journal.Span) error {
				return sourceTask.Run(context.Background(), span)
			}); err != nil {
				t.Fatal(err)
			}
			if err := sourceTask.Finish(context.Background()); err != nil {
				t.Fatal(err)
			}
			group := task.NewLinked(context.Background(), ledger, nil)
			if err := group.StartDomain(bufferTask.Domain(), bufferTask.Run, bufferTask.Sealed()); err != nil {
				t.Fatal(err)
			}
			group.Wait(context.Background())
			stopped := selectFailures(ledger, false)
			if len(stopped) != 1 {
				t.Fatalf("what stopped the edge = %#v, want one consumer failure", stopped)
			}
			if test.panicked {
				var panicErr *journal.PanicError
				if stopped[0].Kind != journal.WorkPanic || !errors.As(stopped[0].Err, &panicErr) {
					t.Fatalf("what stopped the edge = %#v, want the consumer panic", stopped)
				}
			} else if stopped[0].Kind != journal.WorkError || stopped[0].Err.Error() != "sink failure" {
				t.Fatalf("what stopped the edge = %#v, want the consumer failure", stopped)
			}
			releases := selectFailures(ledger, true)
			wantReleases := reader.index - 1
			if len(releases) != wantReleases {
				t.Fatalf("reported release failures = %d, want one per value left in the ring (%d accepted)", len(releases), reader.index)
			}
			for _, failure := range releases {
				if failure.Kind != journal.CleanupPanic || failure.Node != "edge" {
					t.Errorf("release failure = %#v, want a cleanup panic attributed to the edge", failure)
				}
			}
		})
	}
}

// Discard is the last cleanup an execution performs, so a payload it cannot
// release is part of the answer. It visits every task and reports the failures
// together rather than returning as though the queues were empty.
func TestDiscardReportsTheReleasesItCouldNotPerform(t *testing.T) {
	type discardSchemaID struct{}
	typ := schema.Define[discardSchemaID](schema.Traits[int]{Drop: func(int) { panic("declared drop panicked") }})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sinkLink, err := drive.NewSink("in", typ).OpenSink(&templateWriter{templateOperator: templateOperator{shape: sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	ledger := journal.NewLedger()
	buffered, bufferTask, err := drive.NewSource("out", typ).Buffer(queue.Limit{Items: 2}, sinkLink, ledger.Domain("buffer", "edge"))
	if err != nil {
		t.Fatal(err)
	}
	// The source fills the edge while its drain task is not running, which is
	// the state Discard exists for: owners queued behind a consumer that has
	// already left.
	reader := &templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2}}
	sourceTask, err := drive.NewSource("out", typ).OpenSource(reader, buffered, ledger.Domain("source", "source"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceTask.Domain().Perform(journal.Run, func(span *journal.Span) error {
		return sourceTask.Run(context.Background(), span)
	}); err != nil {
		t.Fatal(err)
	}
	execution := &Execution{edges: []namedTask{{task: bufferTask}}}
	execution.Discard()
	failures := selectFailures(ledger, true)
	if len(failures) != 2 {
		t.Fatalf("releases reported to the edge = %d, want one per queued owner", len(failures))
	}
	for _, failure := range failures {
		if failure.Operation != journal.Discard {
			t.Errorf("release operation = %v, want the Discard the run performed", failure.Operation)
		}
		if strings.Contains(failure.Error(), "declared drop panicked") {
			t.Error("the discard report exposes the recovered panic value")
		}
	}
}

func TestObservationStrategiesDoNotEvaluateDetailedTraitsWhenOffOrBasic(t *testing.T) {
	type observedSchemaID struct{}
	var sizeCalls, timeCalls, clockCalls atomic.Int32
	typ := schema.Define[observedSchemaID](schema.Traits[int]{
		Size: func(value int) int {
			sizeCalls.Add(1)
			return value
		},
		Time: func(value int) (int64, bool) {
			timeCalls.Add(1)
			return int64(value), true
		},
	})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time {
		clockCalls.Add(1)
		return time.Unix(1, 0)
	}
	for _, mode := range []observe.Mode{observe.Off, observe.Basic} {
		collector := observe.New(mode, observe.Config{HistoryLimit: 8}, clock)
		ledger := journal.NewLedger()
		execution, err := template.BuildObserved(ledger, []flow.Operator{
			&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{2, 3}},
			&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
		}, collector)
		if err != nil {
			t.Fatal(err)
		}
		value := (&island{ledger: ledger, execution: execution}).run(context.Background())
		if !value.succeeded() {
			t.Fatalf("mode %v report = %#v, ledger = %#v", mode, value.report, value.events())
		}
		events := collector.Snapshot()
		if mode == observe.Off && len(events) != 0 {
			t.Fatalf("Off events = %#v", events)
		}
		if mode == observe.Basic && (len(events) != 1 || events[0].Items != 2 || events[0].Bytes != 0 || !events[0].At.IsZero()) {
			t.Fatalf("Basic events = %#v", events)
		}
	}
	if sizeCalls.Load() != 0 || timeCalls.Load() != 0 || clockCalls.Load() != 0 {
		t.Fatalf("Off/Basic detailed work = size %d time %d clock %d", sizeCalls.Load(), timeCalls.Load(), clockCalls.Load())
	}

	collector := observe.New(observe.Detailed, observe.Config{HistoryLimit: 8}, clock)
	detailedLedger := journal.NewLedger()
	execution, err := template.BuildObserved(detailedLedger, []flow.Operator{
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{2, 3}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	}, collector)
	if err != nil {
		t.Fatal(err)
	}
	detailed := (&island{ledger: detailedLedger, execution: execution}).run(context.Background())
	if !detailed.succeeded() {
		t.Fatalf("Detailed report = %#v, ledger = %#v", detailed.report, detailed.events())
	}
	events := collector.Snapshot()
	if len(events) != 1 || events[0].Items != 2 || events[0].Bytes != 5 || !events[0].HasMedia || events[0].Media != 3 {
		t.Fatalf("Detailed events = %#v", events)
	}
	if sizeCalls.Load() != 2 || timeCalls.Load() != 2 || clockCalls.Load() != 2 {
		t.Fatalf("Detailed work = size %d time %d clock %d", sizeCalls.Load(), timeCalls.Load(), clockCalls.Load())
	}
}
