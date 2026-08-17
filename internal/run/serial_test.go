package run

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
)

type serialOrdinalJoiner struct {
	templateOperator
	out flow.Item[int]

	mu      sync.Mutex
	inputs  []int
	flushes atomic.Int32
}

func (j *serialOrdinalJoiner) Process(ctx context.Context, batch flow.Batch[int], output flow.Emitter[int]) error {
	input, ok := batch.Input()
	item := batch.At(0)
	if !ok || batch.Len() != 1 || item == nil || !item.Valid() {
		return errors.New("invalid serial batch")
	}
	j.mu.Lock()
	j.inputs = append(j.inputs, input)
	j.mu.Unlock()
	output.Own(&j.out, item.Value())
	defer j.out.Drop()
	return output.Emit(ctx, &j.out)
}

func (j *serialOrdinalJoiner) Flush(context.Context, flow.Emitter[int]) error {
	j.flushes.Add(1)
	return nil
}

func (j *serialOrdinalJoiner) Inputs() []int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]int(nil), j.inputs...)
}

func TestSerialFanInBuildUsesNoTaskAndAcceptsOneInput(t *testing.T) {
	typ := templateInput
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.SerialFanIn))},
		[]flow.Port{flow.Out("out", typ)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "join", Shape: joinShape, Execution: drive.NewJoiner("in", typ, flow.SerialFanIn, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("join", "in")),
			job.Connect(job.At("join", "out"), job.At("sink", "in")),
		},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	runtime := template.Projection()
	if len(runtime.FanIns) != 1 || runtime.FanIns[0].Policy != flow.SerialFanIn || runtime.FanIns[0].Tolerance != 0 {
		t.Fatalf("serial fan-in projection = %#v", runtime.FanIns)
	}
	for _, buffer := range runtime.Buffers {
		if buffer.ToNode == "join" && buffer.Reason.Has(plan.FanInBuffer) {
			t.Fatalf("serial fan-in input projected a fan-in buffer: %#v", buffer)
		}
	}
	joiner := &serialOrdinalJoiner{templateOperator: templateOperator{shape: joinShape}}
	writer := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	value := buildIsland(t, template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{4, 5}},
		joiner,
		writer,
	)
	if sources, edgeTasks := value.execution.TaskCounts(); sources != 1 || edgeTasks != 2 {
		t.Fatalf("serial fan-in task counts = sources %d edges %d, want source plus independent buffers", sources, edgeTasks)
	}
	if value.run(context.Background()); !value.succeeded() {
		t.Fatalf("run = %#v, ledger = %#v", value.report, value.events())
	}
	if got := writer.Values(); len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Fatalf("serial fan-in values = %v", got)
	}
	if got := joiner.Inputs(); len(got) != 2 || got[0] != 0 || got[1] != 0 {
		t.Fatalf("serial fan-in input ordinals = %v", got)
	}
	if got := joiner.flushes.Load(); got != 1 {
		t.Fatalf("serial fan-in flushes = %d", got)
	}
}

func TestRouterToSerialFanInPreservesPhysicalInputOrdinal(t *testing.T) {
	typ := templateInput
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	routerShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.SerialFanIn))},
		[]flow.Port{flow.Out("out", typ)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	descriptor := stream.MustDescriptor("route", typ.Descriptor(), timing.Base{}, property.New())
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor)), Execution: drive.NewSource("out", typ)},
			{ID: "router", Shape: routerShape, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor), flow.Describe("out", descriptor)), Execution: drive.NewRouter("in", typ, "out", typ)},
			{ID: "join", Shape: joinShape, Execution: drive.NewJoiner("in", typ, flow.SerialFanIn, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("router", "in")),
			job.Connect(job.At("router", "out"), job.At("join", "in")),
			job.Connect(job.At("join", "out"), job.At("sink", "in")),
		},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range template.connections {
		if template.nodes[connection.from].id == "router" && template.nodes[connection.to].id == "join" && connection.reason != 0 {
			t.Fatalf("router serial input has a buffer reason: %#v", connection)
		}
	}
	for _, buffer := range template.Projection().Buffers {
		if buffer.FromNode == "router" && buffer.ToNode == "join" {
			t.Fatalf("router serial input projected a buffer: %#v", buffer)
		}
	}
	joiner := &serialOrdinalJoiner{templateOperator: templateOperator{shape: joinShape}}
	writer := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	value := buildIsland(t, template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{0, 1, 2, 3}},
		&templateRouter{templateOperator: templateOperator{shape: routerShape}},
		joiner,
		writer,
	)
	if sources, edgeTasks := value.execution.TaskCounts(); sources != 1 || edgeTasks != 2 {
		t.Fatalf("router serial task counts = sources %d edges %d, want source and join output buffers", sources, edgeTasks)
	}
	value.run(context.Background())
	if !value.succeeded() {
		t.Fatalf("run = %#v, ledger = %#v", value.report, value.events())
	}
	if got := joiner.Inputs(); len(got) != 4 || got[0] != 0 || got[1] != 1 || got[2] != 0 || got[3] != 1 {
		t.Fatalf("router serial fan-in ordinals = %v", got)
	}
	if got := writer.Values(); len(got) != 4 || got[0] != 0 || got[1] != 1 || got[2] != 2 || got[3] != 3 {
		t.Fatalf("router serial fan-in values = %v", got)
	}
}

func TestSerialFanInDoesNotRequireTimeBaseAlignmentAndIgnoresZipTolerance(t *testing.T) {
	type timedID struct{}
	typ := schema.Define[timedID](schema.Traits[int]{Time: func(value int) (int64, bool) { return int64(value), true }})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.SerialFanIn))},
		[]flow.Port{flow.Out("out", typ)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	first := stream.MustDescriptor("first", typ.Descriptor(), timing.MustBase(1, 1_000), property.New())
	second := stream.MustDescriptor("second", typ.Descriptor(), timing.MustBase(1, 48_000), property.New())
	nodes := []Node{
		{ID: "a", Shape: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", first)), Execution: drive.NewSource("out", typ)},
		{ID: "b", Shape: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", second)), Execution: drive.NewSource("out", typ)},
		{ID: "join", Shape: joinShape, Outputs: flow.NewDescriptors(flow.Describe("out", first)), Execution: drive.NewJoiner("in", typ, flow.SerialFanIn, "out", typ)},
		{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
	}
	edges := []job.Edge{
		job.Connect(job.At("a", "out"), job.At("join", "in")),
		job.Connect(job.At("b", "out"), job.At("join", "in")),
		job.Connect(job.At("join", "out"), job.At("sink", "in")),
	}
	if _, err := compileFixture(nodes, edges, job.QueuePolicy{Items: 2, Span: time.Millisecond}, job.AlignmentPolicy{}); err != nil {
		t.Fatalf("serial fan-in rejected independent time bases: %v", err)
	}
	template, err := compileFixture(nodes, edges, job.QueuePolicy{Items: 2}, job.AlignmentPolicy{Zip: time.Millisecond})
	if err != nil {
		t.Fatalf("serial fan-in rejected a Zip tolerance policy: %v", err)
	}
	if got := template.Projection().FanIns; len(got) != 1 || got[0].Tolerance != 0 {
		t.Fatalf("serial fan-in projected a Zip tolerance: %#v", got)
	}
}
