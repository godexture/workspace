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
	"github.com/godexture/godec/media/schema"
)

// A bounded edge's Flush happens in the drain task's own goroutine while the
// run may be doing anything else. Nothing outside that goroutine reads or
// writes the span it opens for it, so this runs under -race with the whole
// pipeline live rather than driving the task by hand.
func TestABufferedFlushIsPerformedWithoutASharedSpan(t *testing.T) {
	type bufferedFlushID struct{}
	typ := schema.Define[bufferedFlushID](schema.Traits[int]{Size: func(int) int { return 1 }})
	template, sourceShape, processorShape, sinkShape := bufferedProcessorTemplate(t, typ)
	values := make([]int, 64)
	value := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: values},
		&flushFailingProcessor{templateOperator: templateOperator{shape: processorShape}, typ: typ},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	var found *journal.Failure
	for index, event := range value.events() {
		if errors.Is(event.Err, errBufferedFlush) {
			if found != nil {
				t.Fatalf("the flush failure was recorded more than once: %#v", value.events())
			}
			found = &value.events()[index]
		}
	}
	if found == nil {
		t.Fatalf("events = %#v, want the downstream Flush failure the drain task performed", value.events())
	}
	if found.Operation != journal.Flush {
		t.Fatalf("operation = %v, want Flush", found.Operation)
	}
}

func TestCancellationDoesNotFlushAnAbandonedBuffer(t *testing.T) {
	type cancelledBufferID struct{}
	typ := schema.Define[cancelledBufferID](schema.Traits[int]{Size: func(int) int { return 1 }})
	template, sourceShape, processorShape, sinkShape := bufferedProcessorTemplate(t, typ)
	ctx, cancel := context.WithCancel(context.Background())
	processor := &flushAfterCancellationProcessor{templateOperator: templateOperator{shape: processorShape}}
	value := runIsland(t, ctx, template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: make([]int, 64)},
		processor,
		&blockingSink{templateOperator: templateOperator{shape: sinkShape}, cancel: cancel},
	)
	if processor.flushes.Load() != 0 {
		t.Fatalf("Flush calls after cancellation = %d, want none", processor.flushes.Load())
	}
	for _, event := range value.events() {
		if errors.Is(event.Err, errFlushAfterCancellation) {
			t.Fatalf("cancellation flushed the abandoned path: %#v", value.events())
		}
	}
}

func bufferedProcessorTemplate(t testing.TB, typ schema.Type[int]) (Template, flow.Shape, flow.Shape, flow.Shape) {
	t.Helper()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "proc", Shape: processorShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("proc", "in")),
			job.Connect(job.At("proc", "out"), job.At("sink", "in")),
		},
		job.QueuePolicy{Items: 2},
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return template, sourceShape, processorShape, sinkShape
}

var errBufferedFlush = errors.New("downstream flush failed")
var errFlushAfterCancellation = errors.New("processor requires finalization before flush")

type flushFailingProcessor struct {
	templateOperator
	typ schema.Type[int]
}

func (p *flushFailingProcessor) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	defer input.Drop()
	var item flow.Item[int]
	output.Own(&item, input.Value())
	defer item.Drop()
	return output.Emit(ctx, &item)
}

func (*flushFailingProcessor) Flush(context.Context, flow.Emitter[int]) error {
	return errBufferedFlush
}

type flushAfterCancellationProcessor struct {
	templateOperator
	flushes atomic.Int32
}

func (p *flushAfterCancellationProcessor) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	defer input.Drop()
	var item flow.Item[int]
	output.Own(&item, input.Value())
	defer item.Drop()
	return output.Emit(ctx, &item)
}

func (p *flushAfterCancellationProcessor) Flush(context.Context, flow.Emitter[int]) error {
	p.flushes.Add(1)
	return errFlushAfterCancellation
}

// Two independent failures, one of which is what cancelled the run, both
// survive. The other is not the first one's echo, and nothing about it
// resembling the cancellation cause can make it disappear.
func TestTwoIndependentTaskFailuresBothReachTheLedger(t *testing.T) {
	type independentID struct{}
	typ := schema.Define[independentID](schema.Traits[int]{Size: func(int) int { return 1 }})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", typ)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := compileFixture(
		[]Node{
			{ID: "a", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "b", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "join", Shape: joinShape, Execution: drive.NewJoiner("in", typ, flow.ZipFanIn, "out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{
			job.Connect(job.At("a", "out"), job.At("join", "in")),
			job.Connect(job.At("b", "out"), job.At("join", "in")),
			job.Connect(job.At("join", "out"), job.At("sink", "in")),
		},
		job.QueuePolicy{Items: 4},
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Both readers fail on their own, with the same sentinel, at the same
	// point in their own streams. Neither is the other's consequence.
	shared := errors.New("both readers failed the same way")
	value := runIsland(t, context.Background(), template,
		&refusingReader{templateOperator: templateOperator{shape: sourceShape}, failure: shared},
		&refusingReader{templateOperator: templateOperator{shape: sourceShape}, failure: shared},
		&matrixJoiner{templateOperator{joinShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	count := 0
	for _, event := range value.failures() {
		if errors.Is(event.Err, shared) {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("the shared sentinel was recorded %d times, want one per reader: %#v", count, value.events())
	}
}

type refusingReader struct {
	templateOperator
	failure error
	reads   atomic.Int32
}

func (r *refusingReader) Read(context.Context, *flow.Item[int]) error {
	r.reads.Add(1)
	return r.failure
}

// A run cancelled from outside reports one reason. Every task notices, and
// noticing is not failing: what stopped the run is for the boundary that
// stopped it to say.
func TestAnExternalCancellationIsReportedOnce(t *testing.T) {
	type cancelID struct{}
	typ := schema.Define[cancelID](schema.Traits[int]{Size: func(int) int { return 1 }})
	template, sourceShape, sinkShape := linearTemplate(t, typ, job.QueuePolicy{Items: 2})
	ctx, cancel := context.WithCancel(context.Background())
	values := make([]int, 4096)
	blocking := &blockingSink{templateOperator: templateOperator{shape: sinkShape}, cancel: cancel}
	value := runIsland(t, ctx, template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: values},
		blocking,
	)
	stopped := value.failures()
	if len(stopped) != 1 {
		t.Fatalf("what stopped the run = %#v, want exactly one reason", stopped)
	}
	if !errors.Is(stopped[0].Err, context.Canceled) {
		t.Fatalf("stop reason = %v, want the cancellation", stopped[0].Err)
	}
}

func TestANonComparableExternalCancellationIsReportedOnce(t *testing.T) {
	type cancelID struct{}
	typ := schema.Define[cancelID](schema.Traits[int]{Size: func(int) int { return 1 }})
	template, sourceShape, sinkShape := linearTemplate(t, typ, job.QueuePolicy{Items: 2})
	ctx, cancel := context.WithCancelCause(context.Background())
	values := make([]int, 4096)
	blocking := &blockingSink{
		templateOperator: templateOperator{shape: sinkShape},
		cancel:           func() { cancel(nonComparableCancellation{values: []string{"caller"}}) },
	}
	value := runIsland(t, ctx, template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: values},
		blocking,
	)
	if stopped := value.failures(); len(stopped) != 1 {
		t.Fatalf("non-comparable external cancellation produced %#v, want one failure", value.events())
	}
}

type nonComparableCancellation struct{ values []string }

func (nonComparableCancellation) Error() string { return "non-comparable cancellation" }

type blockingSink struct {
	templateOperator
	cancel func()
	once   atomic.Bool
}

func (s *blockingSink) Write(ctx context.Context, input *flow.Item[int]) error {
	input.Drop()
	if s.once.CompareAndSwap(false, true) {
		s.cancel()
	}
	<-ctx.Done()
	return context.Cause(ctx)
}
