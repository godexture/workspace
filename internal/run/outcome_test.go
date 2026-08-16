package run

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/internal/task"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/schema"
)

type consumerPanic struct{ Token string }
type releasePanic struct{ Token string }

type panickingSink struct{ templateOperator }

func (panickingSink) Write(context.Context, *flow.Item[int]) error {
	panic(consumerPanic{Token: outcomeSecret})
}

const outcomeSecret = "outcome-panic-secret"

// The value a task is holding is released by a deferred Drop that runs while
// the panic which stopped the task is already unwinding. A release that fails
// there used to become the panic, replacing the failure that actually stopped
// the work; both belong in the outcome, in the halves they belong to.
func TestAFailedReleaseDoesNotReplaceTheFailureThatStoppedTheTask(t *testing.T) {
	type inFlightID struct{}
	typ := schema.Define[inFlightID](schema.Traits[int]{
		Drop: func(int) { panic(releasePanic{Token: outcomeSecret}) },
	})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sinkLink, err := drive.NewSink("in", typ).OpenSink(panickingSink{templateOperator{shape: sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	reader := &templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}}
	sourceTask, err := drive.NewSource("out", typ).OpenSource(reader, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	scope := journal.New(journal.Run, "sink")
	sourceTask.BindScope(scope)
	sinkLink.BindScope(scope)
	group := task.New(context.Background())
	if err := group.StartScoped("source", scope, sourceTask.Run); err != nil {
		t.Fatal(err)
	}

	report := group.Wait(context.Background())
	if len(report.Outcomes) != 1 {
		t.Fatalf("outcomes = %#v", report.Outcomes)
	}
	outcome := report.Outcomes[0]
	if outcome.Primary == nil || outcome.Primary.Kind != journal.TaskPanic {
		t.Fatalf("primary = %v, want the panic that stopped the task", outcome.Primary)
	}
	var panicErr *journal.PanicError
	if !errors.As(outcome.Primary.Err, &panicErr) || !strings.Contains(panicErr.Summary, "consumerPanic") {
		t.Fatalf("primary = %v, want the consumer's panic rather than the release that failed beside it", outcome.Primary)
	}
	if len(outcome.Cleanup) != 1 || outcome.Cleanup[0].Kind != journal.CleanupPanic {
		t.Fatalf("cleanup = %#v, want the release the task could not perform", outcome.Cleanup)
	}
	if len(outcome.Primary.Stack) == 0 {
		t.Error("the primary lost the stack it panicked from")
	}
	// Neither half renders a value the panicking code chose.
	for _, rendered := range []string{outcome.Primary.Error(), outcome.Cleanup[0].Error()} {
		if strings.Contains(rendered, outcomeSecret) {
			t.Error("the outcome exposes a recovered panic value")
		}
	}
}

// execution.go's Source case binds the downstream chain's scope but must
// also bind the source task itself: the value Read fills is the source's own
// slot, released by its own deferred Drop, not by anything downstream of
// Emit. Without that second bind -- present for the Joiner case, missing for
// Source -- a release that fails there reports to a scope nothing seals or
// collects.
func TestASourceTasksOwnItemSlotIsBoundSoADeclinedReleaseIsReported(t *testing.T) {
	type oversizedID struct{}
	typ := schema.Define[oversizedID](schema.Traits[int]{
		Size: func(int) int { return 1000 },
		Drop: func(int) { panic(releasePanic{Token: outcomeSecret}) },
	})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := Compile(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
		job.QueuePolicy{Items: 4, Bytes: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := template.Build([]flow.Operator{
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The queue's byte limit rejects the push before anything is ring-full or
	// cancelled: the source's own item stays valid, and its deferred Drop --
	// which this schema declares to panic -- is what must be reported.
	report := runTestExecution(context.Background(), execution)
	var found *journal.Failure
	for outcomeIndex := range report.Outcomes {
		outcome := &report.Outcomes[outcomeIndex]
		for cleanupIndex := range outcome.Cleanup {
			var releaseErr *flow.ReleaseError
			if errors.As(outcome.Cleanup[cleanupIndex].Err, &releaseErr) && strings.Contains(releaseErr.Summary, "releasePanic") {
				found = &outcome.Cleanup[cleanupIndex]
			}
		}
	}
	if found == nil {
		t.Fatalf("outcomes = %#v, want one carrying the source's own declined item release", report.Outcomes)
	}
	if found.Kind != journal.CleanupPanic {
		t.Fatalf("cleanup kind = %v, want CleanupPanic", found.Kind)
	}
}

type failingFlushProcessor struct{ templateOperator }

var errOutcomeFlush = errors.New("processor flush failed")

func (p *failingFlushProcessor) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	item := flow.NewItem(input.Value(), templateOutput, &testDomain)
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	input.Drop()
	return nil
}

func (*failingFlushProcessor) Flush(context.Context, flow.Emitter[int]) error { return errOutcomeFlush }

// A bounded edge's downstream close -- and whatever Flush it triggers -- runs
// inside that edge's own drain-task goroutine when the ring reaches EOF, not
// inside a journal Host opens from Execution.Finish: the drain task may still
// be running when Finish is called, and touching its journal from Host's
// goroutine would race with whatever the drain task is doing to that same
// journal. It is still a genuine Flush, though, so the drain task relabels its
// own journal for this call rather than leaving the failure misattributed to
// Run, and every event still carries the identity its Task and Seq give it
// regardless of the label.
func TestABoundedEdgesOwnFlushFailureSurfacesUnderTheFlushOperation(t *testing.T) {
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", templateInput)}, []flow.Port{flow.Out("out", templateOutput)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", templateOutput)}, nil)
	nodes := []Node{
		{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", templateInput)},
		{ID: "proc", Shape: processorShape, Execution: drive.NewProcessor("in", templateInput, "out", templateOutput)},
		{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", templateOutput)},
	}
	edges := []job.Edge{
		job.Connect(job.At("source", "out"), job.At("proc", "in")),
		job.Connect(job.At("proc", "out"), job.At("sink", "in")),
	}
	template, err := Compile(nodes, edges, templateQueue)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := template.Build([]flow.Operator{
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, values: []int{1}},
		&failingFlushProcessor{templateOperator{shape: processorShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := runTestExecution(context.Background(), execution)
	var found *journal.Failure
	for outcomeIndex := range report.Outcomes {
		outcome := &report.Outcomes[outcomeIndex]
		if outcome.Primary != nil && errors.Is(outcome.Primary.Err, errOutcomeFlush) {
			found = outcome.Primary
		}
		for cleanupIndex := range outcome.Cleanup {
			if errors.Is(outcome.Cleanup[cleanupIndex].Err, errOutcomeFlush) {
				found = &outcome.Cleanup[cleanupIndex]
			}
		}
	}
	if found == nil {
		t.Fatalf("outcomes = %#v, want one carrying the processor's flush failure", report.Outcomes)
	}
	if found.Operation != journal.Flush {
		t.Fatalf("operation = %v, want Flush: the drain task relabels its own journal for the downstream close it performs on EOF", found.Operation)
	}
}

// namedTask.flush is the one place a direct chain's Finish error becomes
// visible to Host, and it must become the Outcome's own Primary rather than a
// second, parallel error return: a caller reading only Outcome would
// otherwise never see it. A bounded edge's Flush failure reaches Host the
// same way, through its own journal's Primary, so both paths converge on one
// shape instead of Host needing to know which kind of edge it is looking at.
func TestADirectChainsFlushErrorBecomesTheJournalsOwnPrimary(t *testing.T) {
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", templateInput)}, []flow.Port{flow.Out("out", templateOutput)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", templateOutput)}, nil)
	sinkLink, err := drive.NewSink("in", templateOutput).OpenSink(&templateWriter{templateOperator: templateOperator{shape: sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	processorLink, err := drive.NewProcessor("in", templateInput, "out", templateOutput).
		Prepend(&failingFlushProcessor{templateOperator{shape: processorShape}}, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	reader := &templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: templateInput, values: []int{1}}
	sourceTask, err := drive.NewSource("out", templateInput).OpenSource(reader, processorLink)
	if err != nil {
		t.Fatal(err)
	}
	// execution.go binds both the chain and the source task to the same scope
	// before Run, not after -- namedTask.flush now relabels and reuses that
	// same scope instead of opening a second one.
	scope := journal.New(journal.Run, "")
	sourceTask.BindScope(scope)
	processorLink.BindScope(scope)
	if err := sourceTask.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	scope.Seal()

	named := namedTask{name: "source", task: sourceTask, chain: processorLink, scope: scope}
	outcome := named.flush(context.Background())
	if outcome.Primary == nil || !errors.Is(outcome.Primary.Err, errOutcomeFlush) {
		t.Fatalf("primary = %v, want the processor's flush error", outcome.Primary)
	}
	if outcome.Primary.Operation != journal.Flush {
		t.Fatalf("operation = %v, want Flush", outcome.Primary.Operation)
	}
}

// A panic during a direct chain's Finish is the review's own reproduction:
// namedTask.flush relabels and reseals the journal Run used, and nothing but
// this call seals it a second time. Before Capture, a panic here skipped
// Seal entirely -- Execution.Finish and Host's own recovery unwound straight
// past it -- so whatever the panic's own unwind recorded (a declined item's
// Drop failing, say) never reached anyone. It must survive exactly the way a
// task's own Run panic already does.
func TestADirectChainsFlushPanicStillSealsWhatItRecorded(t *testing.T) {
	type declinedID struct{}
	typ := schema.Define[declinedID](schema.Traits[int]{
		Drop: func(int) { panic(releasePanic{Token: outcomeSecret}) },
	})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sinkLink, err := drive.NewSink("in", typ).OpenSink(&decliningSink{templateOperator: templateOperator{shape: sinkShape}, writes: new(atomic.Int32)})
	if err != nil {
		t.Fatal(err)
	}
	processorLink, err := drive.NewProcessor("in", typ, "out", typ).
		Prepend(&panickingFlushProcessor{templateOperator{shape: processorShape}}, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	reader := &templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}}
	sourceTask, err := drive.NewSource("out", typ).OpenSource(reader, processorLink)
	if err != nil {
		t.Fatal(err)
	}
	scope := journal.New(journal.Run, "")
	sourceTask.BindScope(scope)
	processorLink.BindScope(scope)
	if err := sourceTask.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	scope.Seal()

	named := namedTask{name: "source", task: sourceTask, chain: processorLink, scope: scope}
	outcome := named.flush(context.Background())
	if outcome.Primary == nil || outcome.Primary.Kind != journal.TaskPanic {
		t.Fatalf("primary = %#v, want the processor's flush panic", outcome.Primary)
	}
	if len(outcome.Cleanup) != 1 || outcome.Cleanup[0].Kind != journal.CleanupPanic {
		t.Fatalf("cleanup = %#v, want the declined item's Drop panic recorded during the unwind", outcome.Cleanup)
	}
}

type retainingProcessor struct {
	templateOperator
	retained flow.Item[int]
}

func (p *retainingProcessor) Process(_ context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	output.Own(&p.retained, input.Value())
	input.Drop()
	return nil
}

func (p *retainingProcessor) Flush(context.Context, flow.Emitter[int]) error {
	p.retained.Drop()
	return nil
}

// A collector or transport that keeps a cell across calls -- runtime.md's own
// documented ownership pattern -- binds that cell once, the first time it is
// filled, during Run. namedTask.flush must not lose the release Flush
// performs on it just because the cell was bound before the Run/Flush
// boundary rather than after: the cell's reporter is whatever it was bound to
// then, and nothing rebinds a slot that already holds a payload.
func TestARetainedCellsReleaseSurvivesTheRunFlushBoundary(t *testing.T) {
	type retainedID struct{}
	typ := schema.Define[retainedID](schema.Traits[int]{
		Drop: func(int) { panic(releasePanic{Token: outcomeSecret}) },
	})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sinkLink, err := drive.NewSink("in", typ).OpenSink(&templateWriter{templateOperator: templateOperator{shape: sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	processorLink, err := drive.NewProcessor("in", typ, "out", typ).
		Prepend(&retainingProcessor{templateOperator: templateOperator{shape: processorShape}}, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	reader := &templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}}
	sourceTask, err := drive.NewSource("out", typ).OpenSource(reader, processorLink)
	if err != nil {
		t.Fatal(err)
	}
	scope := journal.New(journal.Run, "source")
	sourceTask.BindScope(scope)
	processorLink.BindScope(scope)
	if err := sourceTask.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Run's own Capture would seal this scope the moment the source's
	// goroutine returns, well before Host ever calls Finish.
	scope.Seal()

	named := namedTask{name: "source", task: sourceTask, chain: processorLink, scope: scope}
	outcome := named.flush(context.Background())
	if outcome.Primary != nil {
		t.Fatalf("primary = %#v, want none: nothing but the retained cell's release failed", outcome.Primary)
	}
	if len(outcome.Cleanup) != 1 || outcome.Cleanup[0].Kind != journal.CleanupPanic {
		t.Fatalf("cleanup = %#v, want the retained cell's declined release reported to the Outcome Flush seals", outcome.Cleanup)
	}
}

type panickingFlushProcessor struct{ templateOperator }

func (p *panickingFlushProcessor) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	var item flow.Item[int]
	output.Own(&item, input.Value())
	input.Drop()
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	return nil
}

func (*panickingFlushProcessor) Flush(_ context.Context, output flow.Emitter[int]) error {
	var item flow.Item[int]
	output.Own(&item, 1)
	defer item.Drop()
	panic(consumerPanic{Token: outcomeSecret})
}

// A source that hands each value straight to the next stage finishes the value
// where it emitted it. Releasing at the next Read instead would let a failed
// release pass as a read that had not happened, keep the stream running over a
// broken release trait, and leave the slot full at EOF -- which reads as a
// Reader that returned an item together with its EOF.
func TestADirectSourceStopsWhenADeclinedValueCannotBeReleased(t *testing.T) {
	type declinedID struct{}
	var released, written atomic.Int32
	typ := schema.Define[declinedID](schema.Traits[int]{
		Drop: func(int) {
			released.Add(1)
			panic(releasePanic{Token: outcomeSecret})
		},
	})
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sink := &decliningSink{templateOperator: templateOperator{shape: sinkShape}, writes: &written}
	sinkLink, err := drive.NewSink("in", typ).OpenSink(sink)
	if err != nil {
		t.Fatal(err)
	}
	reader := &templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2}}
	sourceTask, err := drive.NewSource("out", typ).OpenSource(reader, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	scope := journal.New(journal.Run, "sink")
	sourceTask.BindScope(scope)
	sinkLink.BindScope(scope)
	if err := sourceTask.Run(context.Background()); err != nil {
		t.Fatalf("run = %v, want the stream to end without a primary failure", err)
	}
	if written.Load() != 1 {
		t.Fatalf("writes = %d, want the stream to stop at the value it could not release", written.Load())
	}
	outcome := scope.Seal()
	if outcome.Primary != nil {
		t.Fatalf("primary = %v, want none: nothing but the release failed", outcome.Primary)
	}
	if len(outcome.Cleanup) != 1 || outcome.Cleanup[0].Kind != journal.CleanupPanic {
		t.Fatalf("cleanup = %#v, want the release the source could not perform", outcome.Cleanup)
	}
	if released.Load() != 1 {
		t.Fatalf("release attempts = %d", released.Load())
	}
}

// Seal is terminal: a later lifecycle operation over the same slots opens its
// own journal rather than continuing this one. But a write that reaches a
// sealed journal anyway is a contract violation this package cannot prevent,
// and it must not compound that by losing the evidence too.
func TestSealIsTerminalButDoesNotDiscardALateWrite(t *testing.T) {
	scope := journal.New(journal.Run, "node")
	scope.Cleanup(errors.New("during the run"))
	first := scope.Seal()
	if len(first.Cleanup) != 1 {
		t.Fatalf("first outcome = %#v", first.Cleanup)
	}
	if !scope.Sealed() {
		t.Fatal("Sealed() did not report the journal as ended")
	}
	scope.Cleanup(errors.New("reported after Seal"))
	late := scope.Seal()
	if len(late.Cleanup) != 1 {
		t.Fatalf("a write that reached a sealed journal was discarded: %#v", late.Cleanup)
	}
}

// Two releases that failed the same way in the same place are two payloads
// that were not released. An identity keeps them apart where their text
// cannot, and stays apart across the Run and the Flush that follows it over
// the same slots: both start counting Seq from one, and only a distinct
// Attempt -- assigned fresh to every Scope, regardless of what Operation it
// carries -- keeps their first events from colliding.
func TestIndependentFailuresKeepSeparateIdentities(t *testing.T) {
	run := journal.New(journal.Run, "node")
	same := errors.New("the same release failure")
	run.Cleanup(same)
	run.Cleanup(same)
	runOutcome := run.Seal()
	if len(runOutcome.Cleanup) != 2 {
		t.Fatalf("cleanup = %#v, want both events", runOutcome.Cleanup)
	}
	first, second := runOutcome.Cleanup[0].ID, runOutcome.Cleanup[1].ID
	if first.Attempt != second.Attempt || first.Seq == second.Seq {
		t.Fatalf("identities = %+v and %+v, want one attempt and two events", first, second)
	}

	flush := journal.New(journal.Flush, "node")
	flush.Attach(run.Identity(), run.Name())
	flush.Cleanup(same)
	flushOutcome := flush.Seal()
	third := flushOutcome.Cleanup[0].ID
	if third.Task != first.Task {
		t.Fatalf("flush identity = %+v, want the same task as the run it followed", third)
	}
	if third.Attempt == first.Attempt {
		t.Fatal("the run and the flush over the same slots share an attempt identity")
	}
	if third == first || third == second {
		t.Fatalf("the flush's first event collides with a run event: %+v", third)
	}
	if flushOutcome.Cleanup[0].Operation != journal.Flush || runOutcome.Cleanup[0].Operation != journal.Run {
		t.Fatalf("operations = %v and %v, want each scope's own", runOutcome.Cleanup[0].Operation, flushOutcome.Cleanup[0].Operation)
	}
}

type decliningSink struct {
	templateOperator
	writes *atomic.Int32
}

func (s decliningSink) Write(context.Context, *flow.Item[int]) error {
	s.writes.Add(1)
	return nil
}

// A task name is chosen for people and nothing keeps two tasks from sharing
// one. Two journals must still keep their events apart, or a consumer that
// reports each event once reports one of two independent failures.
func TestTwoJournalsWithOneNameKeepSeparateIdentities(t *testing.T) {
	same := errors.New("the same release failure")
	group := task.New(context.Background())
	for range 2 {
		scope := journal.New(journal.Run, "node")
		if err := group.StartScoped("worker", scope, func(context.Context) error {
			scope.Cleanup(same)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	report := group.Wait(context.Background())
	if len(report.Outcomes) != 2 {
		t.Fatalf("outcomes = %d, want one per task", len(report.Outcomes))
	}
	first := report.Outcomes[0].Cleanup[0].ID
	second := report.Outcomes[1].Cleanup[0].ID
	if first.Task == second.Task {
		t.Fatalf("both journals identify as task %d, so their events collide", first.Task)
	}
}
