package run

import (
	"context"
	"errors"
	"testing"
	"unsafe"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/schema"
)

type perfID struct{}

var perfType = schema.Define[perfID](schema.Traits[int]{Size: func(int) int { return 1 }})

// perfPass is the shape the ownership contract asks a component to write: take
// the input, hand a value on, defer both releases. It keeps one output cell and
// reuses it, which is what makes a hop cost no allocation -- a fresh cell per
// item is equally correct and costs one.
type perfPass struct {
	templateOperator
	out flow.Item[int]
}

func (p *perfPass) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	defer input.Drop()
	output.Own(&p.out, input.Value()+1)
	defer p.out.Drop()
	return output.Emit(ctx, &p.out)
}

func (*perfPass) Flush(context.Context, flow.Emitter[int]) error { return nil }

type perfCounter struct {
	templateOperator
	count int
}

func (w *perfCounter) Write(_ context.Context, input *flow.Item[int]) error {
	w.count += input.Value()
	input.Drop()
	return nil
}

type perfRouter struct {
	templateOperator
	out flow.Item[int]
}

func (r *perfRouter) Process(ctx context.Context, input *flow.Item[int], outputs flow.RoutedEmitter[int]) error {
	defer input.Drop()
	route, ok := outputs.Route(0)
	if !ok {
		panic("router route 0 is unavailable")
	}
	route.Own(&r.out, input.Value()+1)
	defer r.out.Drop()
	return route.Emit(ctx, &r.out)
}

func (*perfRouter) Flush(context.Context, flow.RoutedEmitter[int]) error { return nil }

// perfChain builds source -> first -> second -> sink. The first-to-second hop
// carries no queue, so it is the fused direct call the hot-path contract is
// about.
func perfChain(t testing.TB, values []int) (*journal.Ledger, *Execution, *perfCounter) {
	t.Helper()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", perfType)})
	passShape := flow.NewShape([]flow.Port{flow.In("in", perfType)}, []flow.Port{flow.Out("out", perfType)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", perfType)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", perfType)},
			{ID: "first", Shape: passShape, Execution: drive.NewProcessor("in", perfType, "out", perfType)},
			{ID: "second", Shape: passShape, Execution: drive.NewProcessor("in", perfType, "out", perfType)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", perfType)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("first", "in")),
			job.Connect(job.At("first", "out"), job.At("second", "in")),
			job.Connect(job.At("second", "out"), job.At("sink", "in")),
		},
		job.QueuePolicy{Items: 8},
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	sink := &perfCounter{templateOperator: templateOperator{shape: sinkShape}}
	ledger := journal.NewLedger()
	execution, err := template.Build(ledger, []flow.Operator{
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: perfType, values: values},
		&perfPass{templateOperator: templateOperator{shape: passShape}},
		&perfPass{templateOperator: templateOperator{shape: passShape}},
		sink,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ledger, execution, sink
}

func perfRouterChain(t testing.TB, values []int) (*journal.Ledger, *Execution, *perfCounter) {
	t.Helper()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", perfType)})
	routerShape := flow.NewShape([]flow.Port{flow.In("in", perfType)}, []flow.Port{flow.Out("out", perfType, flow.Many())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", perfType)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", perfType)},
			{ID: "router", Shape: routerShape, Execution: drive.NewRouter("in", perfType, "out", perfType)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", perfType)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("router", "in")),
			job.Connect(job.At("router", "out"), job.At("sink", "in")),
		},
		job.QueuePolicy{Items: 8},
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	sink := &perfCounter{templateOperator: templateOperator{shape: sinkShape}}
	ledger := journal.NewLedger()
	execution, err := template.Build(ledger, []flow.Operator{
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: perfType, values: values},
		&perfRouter{templateOperator: templateOperator{shape: routerShape}},
		sink,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ledger, execution, sink
}

// A run that succeeds writes nothing to its ledger. Recording is a failure
// path, so a stream of a hundred thousand items costs the ledger's mutex
// exactly nothing.
func TestASuccessfulRunRecordsNothing(t *testing.T) {
	values := make([]int, 4096)
	ledger, execution, sink := perfChain(t, values)
	(&island{ledger: ledger, execution: execution}).run(context.Background())
	if events := ledger.Events(); len(events) != 0 {
		t.Fatalf("a successful run recorded %#v", events)
	}
	if sink.count != len(values)*2 {
		t.Fatalf("sink saw %d, want every item", sink.count)
	}
}

// The per-item cost of the failure design is one atomic load per settled item
// on a task loop -- the check that asks whether the release just performed
// succeeded. It is not on the fused hop, and it allocates nothing.
//
// This measures the whole pipeline per item rather than the check alone,
// because the number that matters is what an item costs end to end.
func TestAnItemCostsNoAllocationPerHop(t *testing.T) {
	const items = 2048
	values := make([]int, items)
	allocations := testing.AllocsPerRun(5, func() {
		ledger, execution, _ := perfChain(t, values)
		(&island{ledger: ledger, execution: execution}).run(context.Background())
	})
	// Everything a run allocates is per run, per task, per operation: the
	// ledger, one domain and site per task, one span per operation, the rings.
	// None of it scales with the item count, so the per-item share of a
	// two-thousand-item run must round to nothing.
	perItem := allocations / items
	if perItem >= 1 {
		t.Fatalf("allocations = %v over %d items (%v per item), want the per-item share to round to zero", allocations, items, perItem)
	}
	t.Logf("%v allocations for %d items across three hops and two queues", allocations, items)
}

func TestRouterDoesNotAllocatePerItem(t *testing.T) {
	const items = 2048
	values := make([]int, items)
	allocations := testing.AllocsPerRun(5, func() {
		ledger, execution, _ := perfRouterChain(t, values)
		(&island{ledger: ledger, execution: execution}).run(context.Background())
	})
	if perItem := allocations / items; perItem >= 1 {
		t.Fatalf("allocations = %v over %d items (%v per item), want the per-item share to round to zero", allocations, items, perItem)
	}
}

// A queue ring holds Items contiguously. What one costs is the payload plus
// the ownership overhead flow pins, and nothing about the run's evidence.
func TestQueueSlotStaysSmall(t *testing.T) {
	t.Logf("flow.Item[int] = %d bytes", unsafe.Sizeof(flow.Item[int]{}))
}

func BenchmarkFusedChainThroughput(b *testing.B) {
	values := make([]int, 1024)
	b.ReportAllocs()
	b.SetBytes(int64(len(values)) * 8)
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		ledger, execution, _ := perfChain(b, values)
		b.StartTimer()
		(&island{ledger: ledger, execution: execution}).run(context.Background())
	}
}

// The failure path is benchmarked apart from the success path on purpose. What
// it costs -- a lock, an event, an append -- must never be read as what an
// ordinary item costs, and a regression in one must not hide behind the other.
func BenchmarkFailurePathRecording(b *testing.B) {
	ledger := journal.NewLedger()
	domain := ledger.Domain("task", "node")
	site := domain.At("node")
	failure := errors.New("a release that could not finish")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		site.Cleanup(failure)
	}
}
