package run

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
)

type templateRouter struct {
	templateOperator
	out     flow.Item[int]
	invalid bool
	panic   bool
	flushes atomic.Int32
}

func (r *templateRouter) Process(ctx context.Context, input *flow.Item[int], outputs flow.RoutedEmitter[int]) error {
	defer input.Drop()
	if r.panic {
		panic("router panic")
	}
	route := input.Value() % 2
	if r.invalid {
		route = 2
	}
	emitter, ok := outputs.Route(route)
	if !ok {
		return nil
	}
	emitter.Own(&r.out, input.Value())
	defer r.out.Drop()
	return emitter.Emit(ctx, &r.out)
}

func (r *templateRouter) Flush(context.Context, flow.RoutedEmitter[int]) error {
	r.flushes.Add(1)
	return nil
}

type templateRouterJoiner struct {
	templateOperator
	out flow.Item[int]
}

func (j *templateRouterJoiner) Process(ctx context.Context, batch flow.Batch[int], output flow.Emitter[int]) error {
	left, leftOK := batch.Value(0)
	right, rightOK := batch.Value(1)
	if !leftOK || !rightOK {
		return errors.New("invalid routed batch")
	}
	output.Own(&j.out, left+right)
	defer j.out.Drop()
	return output.Emit(ctx, &j.out)
}

func (*templateRouterJoiner) Flush(context.Context, flow.Emitter[int]) error { return nil }

type templateFlushRouter struct {
	templateOperator
	out flow.Item[int]
}

func (r *templateFlushRouter) Process(_ context.Context, input *flow.Item[int], _ flow.RoutedEmitter[int]) error {
	input.Drop()
	return nil
}

func (r *templateFlushRouter) Flush(ctx context.Context, outputs flow.RoutedEmitter[int]) error {
	emitter, ok := outputs.Route(0)
	if !ok {
		return errors.New("missing flush route")
	}
	emitter.Own(&r.out, 7)
	defer r.out.Drop()
	return emitter.Emit(ctx, &r.out)
}

func TestRouterBuildsAndDrivesEveryPrivateManyOutputConnection(t *testing.T) {
	typ := templateInput
	sourceDescriptor := stream.MustDescriptor("source", typ.Descriptor(), timing.Base{}, property.New())
	firstDescriptor := stream.MustDescriptor("first", typ.Descriptor(), timing.Base{}, property.New())
	secondDescriptor := stream.MustDescriptor("second", typ.Descriptor(), timing.Base{}, property.New())
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	routerShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", typ)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	edges := []job.Edge{
		job.Connect(job.At("source", "out"), job.At("router", "in")),
		job.Connect(job.At("router", "out"), job.At("join", "in")),
		job.Connect(job.At("join", "out"), job.At("sink", "in")),
	}
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", sourceDescriptor)), Execution: drive.NewSource("out", typ)},
			{ID: "router", Shape: routerShape, Outputs: flow.NewDescriptors(flow.Describe("out", firstDescriptor), flow.Describe("out", secondDescriptor)), Execution: drive.NewRouter("in", typ, "out", typ)},
			{ID: "join", Shape: joinShape, Execution: drive.NewJoiner("in", typ, flow.ZipFanIn, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		edges,
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(template.connections); got != 4 {
		t.Fatalf("physical connections = %d, want source/router + two router routes + join/sink", got)
	}
	routes := make([]int, 0, 2)
	for _, connection := range template.connections {
		if connection.from == 1 && connection.to == 2 {
			routes = append(routes, connection.route)
		}
	}
	if len(routes) != 2 || routes[0] != 0 || routes[1] != 1 {
		t.Fatalf("router routes = %v", routes)
	}
	router := &templateRouter{templateOperator: templateOperator{shape: routerShape}}
	writer := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	result := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{0, 1}},
		router,
		&templateRouterJoiner{templateOperator: templateOperator{shape: joinShape}},
		writer,
	)
	if !result.succeeded() {
		t.Fatalf("run = %#v, ledger = %#v", result.report, result.events())
	}
	if got := writer.Values(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("sink values = %v", got)
	}
	if router.flushes.Load() != 1 {
		t.Fatalf("router flushes = %d", router.flushes.Load())
	}
}

func TestRouterRejectsInvalidOrdinalBeforeOwningOutput(t *testing.T) {
	typ := templateInput
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	routerShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "router", Shape: routerShape, Execution: drive.NewRouter("in", typ, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("router", "in")),
			job.Connect(job.At("router", "out"), job.At("sink", "in")),
		},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	writer := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	result := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{0}},
		&templateRouter{templateOperator: templateOperator{shape: routerShape}, invalid: true},
		writer,
	)
	if !result.succeeded() {
		t.Fatalf("run = %#v, ledger = %#v", result.report, result.events())
	}
	if got := writer.Values(); len(got) != 0 {
		t.Fatalf("invalid route wrote %v", got)
	}
}

func TestRouterFlushDeliversThroughSelectedRoute(t *testing.T) {
	typ := templateInput
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	routerShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "router", Shape: routerShape, Execution: drive.NewRouter("in", typ, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("router", "in")),
			job.Connect(job.At("router", "out"), job.At("sink", "in")),
		},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	writer := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	result := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ},
		&templateFlushRouter{templateOperator: templateOperator{shape: routerShape}},
		writer,
	)
	if !result.succeeded() {
		t.Fatalf("run = %#v, ledger = %#v", result.report, result.events())
	}
	if got := writer.Values(); len(got) != 1 || got[0] != 7 {
		t.Fatalf("flush values = %v", got)
	}
}

func TestRouterGroupsSameRouteLogicalDownstreamsIntoFanout(t *testing.T) {
	typ := templateInput
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	routerShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "router", Shape: routerShape, Execution: drive.NewRouter("in", typ, "out", typ)},
			{ID: "left", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
			{ID: "right", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("router", "in")),
			job.Connect(job.At("router", "out"), job.At("left", "in")),
			job.Connect(job.At("router", "out"), job.At("right", "in")),
		},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	left := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	right := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	result := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{0, 2}},
		&templateRouter{templateOperator: templateOperator{shape: routerShape}},
		left,
		right,
	)
	if !result.succeeded() {
		t.Fatalf("run = %#v, ledger = %#v", result.report, result.events())
	}
	if got := left.Values(); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("left values = %v", got)
	}
	if got := right.Values(); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("right values = %v", got)
	}
}

func TestRouterRequiresExactlyOnePhysicalInput(t *testing.T) {
	typ := templateInput
	descriptor := stream.MustDescriptor("router", typ.Descriptor(), timing.Base{}, property.New())
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	routerShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	_, err := compileFixture(
		[]Node{
			{ID: "first", Shape: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor)), Execution: drive.NewSource("out", typ)},
			{ID: "second", Shape: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor)), Execution: drive.NewSource("out", typ)},
			{ID: "router", Shape: routerShape, Inputs: flow.NewDescriptors(flow.Describe("in", descriptor), flow.Describe("in", descriptor)), Execution: drive.NewRouter("in", typ, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("first", "out"), job.At("router", "in")),
			job.Connect(job.At("second", "out"), job.At("router", "in")),
			job.Connect(job.At("router", "out"), job.At("sink", "in")),
		},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if !errors.Is(err, ErrTopology) {
		t.Fatalf("router with two physical inputs error = %v", err)
	}
}

func TestRouterPanicIsAttributedToRouter(t *testing.T) {
	typ := templateInput
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	routerShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "router", Shape: routerShape, Execution: drive.NewRouter("in", typ, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("router", "in")),
			job.Connect(job.At("router", "out"), job.At("sink", "in")),
		},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{0}},
		&templateRouter{templateOperator: templateOperator{shape: routerShape}, panic: true},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	var panicErr *journal.PanicError
	if failures := result.failures(); len(failures) != 1 || !errors.As(failures[0].Err, &panicErr) || panicErr.Location != "router" {
		t.Fatalf("router panic report = %#v", result.events())
	}
}
