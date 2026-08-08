package run

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/observe"
	"github.com/godexture/godec/internal/run/drive"
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

func (r *templateReader) Read(context.Context) (flow.Input[int], error) {
	if r.index == len(r.values) {
		return flow.Input[int]{}, io.EOF
	}
	typ := r.typ
	if !typ.Valid() {
		typ = templateInput
	}
	value := flow.NewInput(r.values[r.index], typ)
	r.index++
	return value, nil
}

type panickingProcessor struct{ templateOperator }

func (*panickingProcessor) Process(context.Context, flow.Input[int], flow.Emitter[int]) error {
	panic("processor panic")
}

func (*panickingProcessor) Flush(context.Context, flow.Emitter[int]) error { return nil }

type failingWriter struct {
	templateOperator
	failAt int
	writes int
}

func (w *failingWriter) Write(_ context.Context, input flow.Input[int]) error {
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

func (p *templateProcessor) Process(ctx context.Context, input flow.Input[int], output flow.Emitter[int]) error {
	item := flow.NewInput(input.Value()+p.add, p.out)
	if err := output.Emit(ctx, item); err != nil {
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

func (w *templateWriter) Write(_ context.Context, input flow.Input[int]) error {
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

func runTestExecution(ctx context.Context, execution *Execution) task.Report {
	group := task.New(ctx)
	var finishErr error
	if err := execution.Start(group); err != nil {
		return task.Report{Failures: []task.Failure{{Name: "runtime/start", Err: err}}}
	}
	if err := execution.WaitSources(ctx, group); err != nil {
		execution.Close()
		group.Cancel(err)
	} else if err := execution.Quiesce(group.Context()); err != nil {
		execution.Close()
		group.Cancel(err)
	} else if finishErr = execution.Finish(ctx); finishErr != nil {
		group.Cancel(finishErr)
	}
	report := group.Wait(context.Background())
	execution.Discard()
	if finishErr != nil {
		report.Failures = append(report.Failures, task.Failure{Name: "runtime/finish", Err: finishErr})
	}
	return report
}

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
	template, err := Compile(nodes, edges, templateQueue)
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
	template, err := Compile(nodes, edges, templateQueue)
	if err != nil {
		t.Fatal(err)
	}
	runtime := template.Projection()
	if len(runtime.FanIns) != 1 || runtime.FanIns[0].Policy != flow.ZipFanIn || len(runtime.FanIns[0].Connections) != 2 || runtime.FanIns[0].Connections[0].FromNode != "a" || runtime.FanIns[0].Connections[1].FromNode != "b" {
		t.Fatalf("fan-in = %#v", runtime.FanIns)
	}
	if len(runtime.Buffers) != 3 || !runtime.Buffers[0].Reason.Has(plan.FanInBuffer) || !runtime.Buffers[1].Reason.Has(plan.FanInBuffer) {
		t.Fatalf("fan-in buffers = %#v", runtime.Buffers)
	}
}

func TestCompileSelectsTraitAwareQueueLimitsAndFanInWatermark(t *testing.T) {
	type timedID struct{}
	typ := schema.Define[timedID](schema.Traits[int]{
		Size: func(int) int { return 8 },
		Time: func(value int) (int64, bool) { return int64(value), true },
	})
	base := timing.MustBase(1, 1_000)
	descriptor := stream.MustDescriptor("timed", typ.Identity(), base, property.New())
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
	template, err := Compile(nodes, []job.Edge{
		job.Connect(job.At("a", "out"), job.At("join", "in")),
		job.Connect(job.At("b", "out"), job.At("join", "in")),
		job.Connect(job.At("join", "out"), job.At("sink", "in")),
	}, job.QueuePolicy{Items: 2, Bytes: 1 << 20, Window: 250 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	runtime := template.Projection()
	if len(runtime.FanIns) != 1 || runtime.FanIns[0].Watermark != 250 {
		t.Fatalf("fan-in watermark = %#v", runtime.FanIns)
	}
	for _, buffer := range runtime.Buffers {
		if buffer.Limit != (plan.Limit{Items: 2, Bytes: 1 << 20, Time: 250}) {
			t.Fatalf("buffer limit = %#v", buffer.Limit)
		}
	}
}

func TestCompileRejectsNodesOutsideTopologicalOrder(t *testing.T) {
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", templateInput)}, nil)
	_, err := Compile([]Node{
		{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", templateInput)},
		{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", templateInput)},
	}, []job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))}, templateQueue)
	if !errors.Is(err, ErrTopologyOrder) {
		t.Fatalf("topological order error = %v", err)
	}
}

func TestCompileKeepsPlanningOnlyGraphNonExecutable(t *testing.T) {
	template, err := Compile(
		[]Node{
			{ID: "source", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})},
			{ID: "sink", Shape: flow.NewShape([]flow.Port{flow.In("in", templateInput)}, nil)},
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
		templateQueue,
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
	template, err := Compile(nodes, edges, templateQueue)
	if err != nil {
		t.Fatal(err)
	}
	writer := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	execution, err := template.Build([]flow.Operator{
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, values: []int{1, 2, 3}},
		&templateProcessor{templateOperator: templateOperator{shape: firstShape}, in: templateInput, out: templateMiddle, add: 10},
		&templateProcessor{templateOperator: templateOperator{shape: secondShape}, in: templateMiddle, out: templateOutput, add: 100},
		writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	sources, edgeTasks := execution.TaskCounts()
	if sources != 1 || edgeTasks != 2 {
		t.Fatalf("task counts = sources %d edges %d", sources, edgeTasks)
	}
	report := runTestExecution(context.Background(), execution)
	if !report.Complete() || len(report.Failures) != 0 {
		t.Fatalf("run report = %#v", report)
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
	template, err := Compile(
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
	)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := template.Build([]flow.Operator{
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}},
		&panickingProcessor{templateOperator{shape: processorShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := runTestExecution(context.Background(), execution)
	var panicErr *task.PanicError
	if len(report.Failures) != 1 || !errors.As(report.Failures[0].Err, &panicErr) || panicErr.Location != "panic" {
		t.Fatalf("panic report = %#v", report)
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
		template, err := Compile(
			[]Node{
				{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
				{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
			},
			[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
			templateQueue,
		)
		if err != nil {
			t.Fatal(err)
		}
		reader := &templateReader{
			templateOperator: templateOperator{shape: sourceShape},
			typ:              typ,
			values:           make([]int, failAt+templateQueue.Items*2),
		}
		execution, err := template.Build([]flow.Operator{
			reader,
			&failingWriter{templateOperator: templateOperator{shape: sinkShape}, failAt: failAt},
		})
		if err != nil {
			t.Fatal(err)
		}
		report := runTestExecution(context.Background(), execution)
		if len(report.Failures) == 0 {
			t.Fatalf("run %d unexpectedly succeeded", run)
		}
		emitted := int64(reader.index)
		if got := dropped.Load(); got != emitted {
			t.Fatalf("run %d dropped = %d, want %d emitted items", run, got, emitted)
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
	template, err := Compile(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
		templateQueue,
	)
	if err != nil {
		t.Fatal(err)
	}
	clock := func() time.Time {
		clockCalls.Add(1)
		return time.Unix(1, 0)
	}
	for _, mode := range []observe.Mode{observe.Off, observe.Basic} {
		collector := observe.New(mode, clock)
		execution, err := template.BuildObserved([]flow.Operator{
			&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{2, 3}},
			&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
		}, collector)
		if err != nil {
			t.Fatal(err)
		}
		if report := runTestExecution(context.Background(), execution); !report.Complete() || len(report.Failures) != 0 {
			t.Fatalf("mode %v report = %#v", mode, report)
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

	collector := observe.New(observe.Detailed, clock)
	execution, err := template.BuildObserved([]flow.Operator{
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{2, 3}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	}, collector)
	if err != nil {
		t.Fatal(err)
	}
	if report := runTestExecution(context.Background(), execution); !report.Complete() || len(report.Failures) != 0 {
		t.Fatalf("Detailed report = %#v", report)
	}
	events := collector.Snapshot()
	if len(events) != 1 || events[0].Items != 2 || events[0].Bytes != 5 || !events[0].HasMedia || events[0].Media != 3 {
		t.Fatalf("Detailed events = %#v", events)
	}
	if sizeCalls.Load() != 2 || timeCalls.Load() != 2 || clockCalls.Load() != 2 {
		t.Fatalf("Detailed work = size %d time %d clock %d", sizeCalls.Load(), timeCalls.Load(), clockCalls.Load())
	}
}
