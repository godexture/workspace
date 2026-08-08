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
	"github.com/godexture/godec/media/schema"
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
)

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
	template, err := Compile(nodes, edges)
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
	template, err := Compile(nodes, edges)
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

func TestCompileKeepsPlanningOnlyGraphNonExecutable(t *testing.T) {
	template, err := Compile(
		[]Node{
			{ID: "source", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})},
			{ID: "sink", Shape: flow.NewShape([]flow.Port{flow.In("in", templateInput)}, nil)},
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
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
	template, err := Compile(nodes, edges)
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
	report := execution.Run(context.Background())
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
	report := execution.Run(context.Background())
	var panicErr *task.PanicError
	if len(report.Failures) != 1 || !errors.As(report.Failures[0].Err, &panicErr) || panicErr.Location != "panic" {
		t.Fatalf("panic report = %#v", report)
	}
	if drops.Load() != 1 {
		t.Fatalf("panic drop count = %d", drops.Load())
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
		if report := execution.Run(context.Background()); !report.Complete() || len(report.Failures) != 0 {
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
	if report := execution.Run(context.Background()); !report.Complete() || len(report.Failures) != 0 {
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
