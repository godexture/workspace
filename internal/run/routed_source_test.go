package run

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
)

type routedTemplateEmission struct {
	route int
	value int
}

type routedTemplateReader struct {
	templateOperator
	steps   [][]routedTemplateEmission
	index   int
	items   []flow.Item[int]
	failure error
	panic   bool
}

type routedFlushJoiner struct {
	templateRouterJoiner
	flushes atomic.Int32
}

type blockingRoutedTemplateReader struct {
	templateOperator
	started chan struct{}
	once    sync.Once
}

func (r *blockingRoutedTemplateReader) Read(ctx context.Context, _ flow.RoutedEmitter[int]) error {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	return context.Cause(ctx)
}

func (j *routedFlushJoiner) Flush(context.Context, flow.Emitter[int]) error {
	j.flushes.Add(1)
	return nil
}

func (r *routedTemplateReader) Read(ctx context.Context, outputs flow.RoutedEmitter[int]) error {
	if r.panic {
		panic("routed source panic")
	}
	if r.index == len(r.steps) {
		return r.failure
	}
	for _, emission := range r.steps[r.index] {
		emitter, ok := outputs.Route(emission.route)
		if !ok {
			continue
		}
		item := &r.items[emission.route]
		emitter.Own(item, emission.value)
		if err := emitter.Emit(ctx, item); err != nil {
			item.Drop()
			return err
		}
		item.Drop()
	}
	r.index++
	return nil
}

func routedSourceTemplate(t testing.TB) (Template, flow.Shape, flow.Shape, flow.Shape) {
	t.Helper()
	typ := templateInput
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ, flow.Many())})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", typ)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	descriptor := stream.MustDescriptor("route", typ.Descriptor(), timing.Base{}, property.New())
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor), flow.Describe("out", descriptor)), Execution: drive.NewRoutedSource("out", typ)},
			{ID: "join", Shape: joinShape, Execution: drive.NewJoiner("in", typ, flow.ZipFanIn, "out", typ)},
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
	return template, sourceShape, joinShape, sinkShape
}

func TestRoutedSourceBuildsRepeatedDescriptorsInRouteOrder(t *testing.T) {
	template, sourceShape, joinShape, sinkShape := routedSourceTemplate(t)
	if sources, edges := sourceTaskCounts(t, template, sourceShape, joinShape, sinkShape); sources != 1 || edges != 2 {
		t.Fatalf("task counts = sources %d edges %d, want routed source plus zip and join-output buffers", sources, edges)
	}
	routes := make([]int, 0, 2)
	for _, connection := range template.connections {
		if template.nodes[connection.from].id == "source" && template.nodes[connection.to].id == "join" {
			routes = append(routes, connection.route)
			if !connection.reason.Has(plan.SourceBuffer) {
				t.Fatalf("routed source connection missing SourceBuffer: %#v", connection)
			}
		}
	}
	if len(routes) != 2 || routes[0] != 0 || routes[1] != 1 {
		t.Fatalf("route descriptor order = %v", routes)
	}
	for _, buffer := range template.Projection().Buffers {
		if buffer.FromNode == "source" && buffer.ToNode == "join" && !buffer.Reason.Has(plan.SourceBuffer) {
			t.Fatalf("routed source buffer projection = %#v", buffer)
		}
	}
	reader := &routedTemplateReader{
		templateOperator: templateOperator{shape: sourceShape},
		steps: [][]routedTemplateEmission{
			{{route: 1, value: 10}, {route: 0, value: 1}},
			{{route: 0, value: 2}, {route: 1, value: 20}},
		},
		items:   make([]flow.Item[int], 2),
		failure: io.EOF,
	}
	writer := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	result := runIsland(t, context.Background(), template,
		reader,
		&templateRouterJoiner{templateOperator: templateOperator{shape: joinShape}},
		writer,
	)
	if !result.succeeded() {
		t.Fatalf("run = %#v, ledger = %#v", result.report, result.events())
	}
	if got := writer.Values(); len(got) != 2 || got[0] != 11 || got[1] != 22 {
		t.Fatalf("route values = %v", got)
	}
}

func TestRoutedSourceUnavailableRouteStopsWithoutProgress(t *testing.T) {
	template, sourceShape, joinShape, sinkShape := routedSourceTemplate(t)
	reader := &routedTemplateReader{
		templateOperator: templateOperator{shape: sourceShape},
		steps:            [][]routedTemplateEmission{{{route: 2, value: 1}}},
		items:            make([]flow.Item[int], 2),
		failure:          io.EOF,
	}
	result := runIsland(t, context.Background(), template,
		reader,
		&templateRouterJoiner{templateOperator: templateOperator{shape: joinShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	failures := result.failures()
	if len(failures) != 1 || !errors.Is(failures[0].Err, drive.ErrInvalidItem) || failures[0].Node != "source" {
		t.Fatalf("unavailable route failures = %#v", result.events())
	}
}

func TestRoutedSourceEOFClosesAllRoutesAndFlushesDownstream(t *testing.T) {
	template, sourceShape, joinShape, sinkShape := routedSourceTemplate(t)
	joiner := &routedFlushJoiner{templateRouterJoiner: templateRouterJoiner{templateOperator: templateOperator{shape: joinShape}}}
	reader := &routedTemplateReader{
		templateOperator: templateOperator{shape: sourceShape},
		steps:            [][]routedTemplateEmission{{{route: 0, value: 1}, {route: 1, value: 2}}},
		items:            make([]flow.Item[int], 2),
		failure:          io.EOF,
	}
	result := runIsland(t, context.Background(), template,
		reader,
		joiner,
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	if !result.succeeded() {
		t.Fatalf("run = %#v, ledger = %#v", result.report, result.events())
	}
	if got := joiner.flushes.Load(); got != 1 {
		t.Fatalf("downstream Flush calls = %d", got)
	}
}

func TestRoutedSourcePanicIsAttributedToSource(t *testing.T) {
	template, sourceShape, joinShape, sinkShape := routedSourceTemplate(t)
	result := runIsland(t, context.Background(), template,
		&routedTemplateReader{templateOperator: templateOperator{shape: sourceShape}, items: make([]flow.Item[int], 2), panic: true},
		&templateRouterJoiner{templateOperator: templateOperator{shape: joinShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	var panicErr *journal.PanicError
	failures := result.failures()
	if len(failures) != 1 || !errors.As(failures[0].Err, &panicErr) || panicErr.Location != "source" {
		t.Fatalf("routed source panic = %#v", result.events())
	}
}

func TestRoutedSourceExternalCancellationIsReportedOnce(t *testing.T) {
	template, sourceShape, joinShape, sinkShape := routedSourceTemplate(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &blockingRoutedTemplateReader{templateOperator: templateOperator{shape: sourceShape}, started: make(chan struct{})}
	result := buildIsland(t, template,
		reader,
		&templateRouterJoiner{templateOperator: templateOperator{shape: joinShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	done := make(chan struct{})
	go func() {
		result.run(ctx)
		close(done)
	}()
	<-reader.started
	cancel()
	<-done
	failures := result.failures()
	if len(failures) != 1 || !errors.Is(failures[0].Err, context.Canceled) {
		t.Fatalf("cancellation failures = %#v", result.events())
	}
}

func sourceTaskCounts(t testing.TB, template Template, sourceShape, joinShape, sinkShape flow.Shape) (int, int) {
	t.Helper()
	execution, err := template.Build(journal.NewLedger(), []flow.Operator{
		&routedTemplateReader{templateOperator: templateOperator{shape: sourceShape}, items: make([]flow.Item[int], 2), failure: io.EOF},
		&templateRouterJoiner{templateOperator: templateOperator{shape: joinShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return execution.TaskCounts()
}

var _ flow.RoutedReader[int] = (*routedTemplateReader)(nil)
var _ flow.RoutedReader[int] = (*blockingRoutedTemplateReader)(nil)
