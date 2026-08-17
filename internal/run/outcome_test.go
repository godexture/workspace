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
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/schema"
)

type consumerPanic struct{ Token string }
type releasePanic struct{ Token string }

const outcomeSecret = "outcome-panic-secret"

type panickingSink struct{ templateOperator }

func (panickingSink) Write(context.Context, *flow.Item[int]) error {
	panic(consumerPanic{Token: outcomeSecret})
}

// releasing declares a payload whose release always fails, which is the only
// way to observe where a failed release is reported from.
func releasing[ID any]() schema.Type[int] {
	return schema.Define[ID](schema.Traits[int]{
		Drop: func(int) { panic(releasePanic{Token: outcomeSecret}) },
	})
}

func linearTemplate(t testing.TB, typ schema.Type[int], policy job.QueuePolicy) (Template, flow.Shape, flow.Shape) {
	t.Helper()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	template, err := Compile(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
		policy,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return template, sourceShape, sinkShape
}

// releaseFailures returns the recorded releases whose payload declared the
// failing Drop, so a test counts the ones it caused rather than every event.
func releaseFailures(events []journal.Failure) []journal.Failure {
	var result []journal.Failure
	for _, event := range events {
		var release *flow.ReleaseError
		if errors.As(event.Err, &release) && strings.Contains(release.Summary, "releasePanic") {
			result = append(result, event)
		}
	}
	return result
}

func assertNoRawPanicValue(t testing.TB, events []journal.Failure) {
	t.Helper()
	for _, event := range events {
		if strings.Contains(event.Error(), outcomeSecret) {
			t.Errorf("a recorded failure exposes the value the panicking code chose: %v", event)
		}
	}
}

func TestAFailedReleaseDoesNotReplaceTheFailureThatStoppedTheTask(t *testing.T) {
	type inFlightID struct{}
	typ := releasing[inFlightID]()
	template, sourceShape, sinkShape := linearTemplate(t, typ, templateQueue)
	value := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}},
		panickingSink{templateOperator{shape: sinkShape}},
	)
	stopped := value.failures()
	if len(stopped) != 1 || stopped[0].Kind != journal.WorkPanic {
		t.Fatalf("what stopped the run = %#v, want the consumer's panic", stopped)
	}
	var panicErr *journal.PanicError
	if !errors.As(stopped[0].Err, &panicErr) || !strings.Contains(panicErr.Summary, "consumerPanic") {
		t.Fatalf("stop reason = %v, want the consumer's panic rather than the release that failed beside it", stopped[0])
	}
	if len(stopped[0].Stack) == 0 {
		t.Error("the failure lost the stack it panicked from")
	}
	if releases := releaseFailures(value.cleanups()); len(releases) != 1 {
		t.Fatalf("releases = %#v, want the one the task could not perform", value.cleanups())
	}
	assertNoRawPanicValue(t, value.events())
}

// The first of the three defects this design closes.
//
// A Reader may fill the caller's slot and then fail; that slot is the source
// task's own, released by its own deferred Drop, and a release that fails there
// has to reach the same place the Reader's error does. It used to reach a
// journal the source constructed for itself, which nothing sealed and nothing
// collected, so the run reported the Reader's failure and lost the release
// entirely. A task now cannot be constructed without the domain it reports to,
// so there is no shape of this topology that produces an uncollected one.
func TestAReaderThatFailsHoldingAnItemReportsBothOnce(t *testing.T) {
	type readerHeldID struct{}
	var released atomic.Int32
	typ := schema.Define[readerHeldID](schema.Traits[int]{
		Drop: func(int) {
			released.Add(1)
			panic(releasePanic{Token: outcomeSecret})
		},
	})
	readerFailure := errors.New("reader failed holding an item")
	template, sourceShape, sinkShape := linearTemplate(t, typ, templateQueue)
	value := runIsland(t, context.Background(), template,
		&failingReader{templateOperator: templateOperator{shape: sourceShape}, failure: readerFailure},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	stopped := value.failures()
	if len(stopped) != 1 || !errors.Is(stopped[0].Err, readerFailure) {
		t.Fatalf("what stopped the run = %#v, want the Reader's own failure", stopped)
	}
	releases := releaseFailures(value.cleanups())
	if len(releases) != 1 {
		t.Fatalf("releases = %#v, want exactly the one the source could not perform", value.cleanups())
	}
	if releases[0].Kind != journal.CleanupPanic {
		t.Fatalf("release kind = %v, want a cleanup panic", releases[0].Kind)
	}
	if releases[0].Task != "source/source" {
		t.Fatalf("release attributed to %q, want the source task that owned the slot", releases[0].Task)
	}
	if released.Load() != 1 {
		t.Fatalf("release attempts = %d, want exactly one", released.Load())
	}
	assertNoRawPanicValue(t, value.events())
}

// failingReader fills the caller's slot and then reports a failure, which the
// contract allows: Read owns nothing, and what it left in the slot is the
// source task's to release.
type failingReader struct {
	templateOperator
	failure error
	done    bool
}

func (r *failingReader) Read(_ context.Context, into *flow.Item[int]) error {
	if r.done {
		return r.failure
	}
	r.done = true
	into.Set(1)
	return r.failure
}

// retainingProcessor is the collector/transport pattern the ownership contract
// permits: a component that keeps a payload past the call it arrived in. It
// binds its own slot to the Owner the runtime granted it, which is what makes
// that slot's lifetime its own declaration rather than an accident of whichever
// caller handed the payload over.
type retainingProcessor struct {
	templateOperator
	typ       schema.Type[int]
	owner     flow.Owner
	held      flow.Item[int]
	releaseIn journal.Operation
}

func (p *retainingProcessor) Process(_ context.Context, input *flow.Item[int], _ flow.Emitter[int]) error {
	p.held.Bind(p.typ, p.owner)
	p.held.Move(input)
	return nil
}

func (p *retainingProcessor) Flush(context.Context, flow.Emitter[int]) error {
	if p.releaseIn == journal.Flush {
		p.held.Drop()
	}
	return nil
}

func (p *retainingProcessor) Close() error {
	if p.releaseIn == journal.Close {
		p.held.Drop()
	}
	return nil
}

// The second of the three defects.
//
// A retained cell keeps the reporting site it was bound under, and nothing
// rebinds a slot that already holds a payload. When Run and Flush were separate
// journals, such a cell went on reporting into the Run journal after it had
// been sealed and nothing ever read it again -- the evidence stayed in memory
// and never reached the caller. The reporting site now belongs to a domain that
// lives for the whole run, so where the release happens decides only which
// lifecycle step it is recorded under, never whether it is recorded.
func TestARetainedPayloadReleasedAfterRunIsStillCollected(t *testing.T) {
	for _, test := range []struct {
		name    string
		release journal.Operation
	}{
		{name: "released during Flush", release: journal.Flush},
		{name: "released during Close", release: journal.Close},
	} {
		t.Run(test.name, func(t *testing.T) {
			type retainedID struct{}
			typ := releasing[retainedID]()
			sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
			processorShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
			sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
			template, err := Compile(
				[]Node{
					{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", typ)},
					{ID: "keep", Shape: processorShape, Execution: drive.NewProcessor("in", typ, "out", typ)},
					{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
				},
				[]job.Edge{
					job.Connect(job.At("source", "out"), job.At("keep", "in")),
					job.Connect(job.At("keep", "out"), job.At("sink", "in")),
				},
				templateQueue,
				job.AlignmentPolicy{},
			)
			if err != nil {
				t.Fatal(err)
			}
			ledger := journal.NewLedger()
			// The Owner is what Host grants a component at Open: a domain of
			// its own, alive for the whole run.
			keep := &retainingProcessor{
				templateOperator: templateOperator{shape: processorShape},
				typ:              typ,
				owner:            ledger.Domain("node/keep", "keep").At("keep"),
				releaseIn:        test.release,
			}
			execution, err := template.Build(ledger, []flow.Operator{
				&templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}},
				keep,
				&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
			})
			if err != nil {
				t.Fatal(err)
			}
			value := (&island{ledger: ledger, execution: execution}).run(context.Background())
			if test.release == journal.Close {
				// Host closes operators during cleanup, under that stage.
				ledger.EnterStage(journal.Close)
				if err := keep.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if stopped := value.failures(); len(stopped) != 0 {
				t.Fatalf("what stopped the run = %#v, want nothing: only the retained release failed", stopped)
			}
			releases := releaseFailures(value.cleanups())
			if len(releases) != 1 {
				t.Fatalf("releases = %#v, want exactly the retained payload's", value.cleanups())
			}
			if releases[0].Operation != test.release {
				t.Fatalf("release operation = %v, want %v: the step it was released in", releases[0].Operation, test.release)
			}
			if releases[0].Node != "keep" {
				t.Fatalf("release node = %q, want the component that owned it", releases[0].Node)
			}
			assertNoRawPanicValue(t, value.events())
		})
	}
}

// An unbound slot refuses ownership rather than inheriting the sender's domain.
// Inheriting would make the retained payload's lifetime an accident of whichever
// caller handed it over, and would let a component keep a payload reporting into
// a domain that says nothing about where it now lives.
func TestAComponentSlotThatDeclaresNoDomainRefusesOwnership(t *testing.T) {
	type unboundID struct{}
	typ := schema.Define[unboundID](schema.Traits[int]{})
	source := flow.NewItem(1, typ, &testDomain)
	defer source.Drop()
	var held flow.Item[int]
	if held.Move(&source) {
		t.Fatal("an unbound slot took ownership")
	}
	if !source.Valid() || held.Valid() {
		t.Fatal("a refused Move must leave the source holding its payload")
	}
	if held.Fork(&held) || !source.Valid() {
		t.Fatal("a refused Fork must leave the source holding its payload")
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

// A bounded edge's downstream close runs inside that edge's own drain-task
// goroutine when the ring reaches EOF, never in a span the run opens from
// Finish: the drain task may still be running, and touching its domain from
// another goroutine would race whatever it is doing. It is still a genuine
// Flush, so the drain task opens a Flush span of its own inside the Run it is
// executing, and the failure names the operation it belongs to.
func TestABoundedEdgesOwnFlushFailureSurfacesUnderTheFlushOperation(t *testing.T) {
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", templateInput)}, []flow.Port{flow.Out("out", templateOutput)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", templateOutput)}, nil)
	template, err := Compile(
		[]Node{
			{ID: "source", Shape: sourceShape, Execution: drive.NewSource("out", templateInput)},
			{ID: "proc", Shape: processorShape, Execution: drive.NewProcessor("in", templateInput, "out", templateOutput)},
			{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", templateOutput)},
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("proc", "in")),
			job.Connect(job.At("proc", "out"), job.At("sink", "in")),
		},
		templateQueue,
		job.AlignmentPolicy{},
	)
	if err != nil {
		t.Fatal(err)
	}
	value := runIsland(t, context.Background(), template,
		&templateReader{templateOperator: templateOperator{shape: sourceShape}, values: []int{1}},
		&failingFlushProcessor{templateOperator{shape: processorShape}},
		&templateWriter{templateOperator: templateOperator{shape: sinkShape}},
	)
	var found *journal.Failure
	for index, event := range value.events() {
		if errors.Is(event.Err, errOutcomeFlush) {
			found = &value.events()[index]
		}
	}
	if found == nil {
		t.Fatalf("events = %#v, want one carrying the processor's flush failure", value.events())
	}
	if found.Operation != journal.Flush {
		t.Fatalf("operation = %v, want Flush: the drain task opens a Flush span for the close it performs on EOF", found.Operation)
	}
	// One event, however many boundaries observed the cancellation it caused.
	if count := countMatching(value.events(), errOutcomeFlush); count != 1 {
		t.Fatalf("the flush failure was recorded %d times, want once", count)
	}
}

func countMatching(events []journal.Failure, target error) int {
	count := 0
	for _, event := range events {
		if errors.Is(event.Err, target) {
			count++
		}
	}
	return count
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
	output.Own(&item, unreleasable)
	defer item.Drop()
	panic(consumerPanic{Token: outcomeSecret})
}

// unreleasable is the one payload value whose release fails, so a topology can
// carry ordinary values through Run and still observe a release failing at the
// exact boundary a test is about.
const unreleasable = -1

func selective[ID any]() schema.Type[int] {
	return schema.Define[ID](schema.Traits[int]{
		Drop: func(value int) {
			if value == unreleasable {
				panic(releasePanic{Token: outcomeSecret})
			}
		},
	})
}

// A direct chain's Flush is driven by the run rather than by the task's own
// goroutine, and it is the least attended boundary there is. A panic there must
// lose nothing that a task's own panic would not: the panic is what stopped the
// work, the release that failed while it unwound is cleanup, and neither
// renders the value the panicking code chose.
func TestADirectChainsFlushPanicKeepsBothHalves(t *testing.T) {
	type declinedID struct{}
	typ := selective[declinedID]()
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	sinkLink, err := drive.NewSink("in", typ).OpenSink(&templateWriter{templateOperator: templateOperator{shape: sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	processorLink, err := drive.NewProcessor("in", typ, "out", typ).
		Prepend(&panickingFlushProcessor{templateOperator{shape: processorShape}}, sinkLink)
	if err != nil {
		t.Fatal(err)
	}
	ledger := journal.NewLedger()
	reader := &templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1}}
	sourceTask, err := drive.NewSource("out", typ).OpenSource(reader, processorLink, ledger.Domain("source/source", "source"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sourceTask.Domain().Perform(journal.Run, func(span *journal.Span) error {
		return sourceTask.Run(context.Background(), span)
	}); err != nil {
		t.Fatalf("run = %v, want a clean stream: only the Flush is meant to fail", err)
	}

	named := namedTask{task: sourceTask, chain: processorLink}
	if cause := named.flush(context.Background()); cause == nil {
		t.Fatal("a panicking Flush reported no cause")
	}
	stopped := selectFailures(ledger, false)
	if len(stopped) != 1 || stopped[0].Kind != journal.WorkPanic {
		t.Fatalf("what stopped the flush = %#v, want the processor's panic", stopped)
	}
	if stopped[0].Operation != journal.Flush {
		t.Fatalf("operation = %v, want Flush", stopped[0].Operation)
	}
	releases := releaseFailures(selectFailures(ledger, true))
	if len(releases) != 1 || releases[0].Kind != journal.CleanupPanic {
		t.Fatalf("releases = %#v, want the declined item's Drop recorded during the unwind", selectFailures(ledger, true))
	}
	assertNoRawPanicValue(t, ledger.Events())
}

type decliningSink struct {
	templateOperator
	writes *atomic.Int32
}

func (s decliningSink) Write(context.Context, *flow.Item[int]) error {
	s.writes.Add(1)
	return nil
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
	ledger := journal.NewLedger()
	reader := &templateReader{templateOperator: templateOperator{shape: sourceShape}, typ: typ, values: []int{1, 2}}
	sourceTask, err := drive.NewSource("out", typ).OpenSource(reader, sinkLink, ledger.Domain("source/source", "source"))
	if err != nil {
		t.Fatal(err)
	}
	cause := sourceTask.Domain().Perform(journal.Run, func(span *journal.Span) error {
		return sourceTask.Run(context.Background(), span)
	})
	if written.Load() != 1 {
		t.Fatalf("writes = %d, want the stream to stop at the value it could not release", written.Load())
	}
	if len(selectFailures(ledger, false)) != 0 {
		t.Fatalf("what stopped the source = %#v, want nothing: only the release failed", selectFailures(ledger, false))
	}
	releases := selectFailures(ledger, true)
	if len(releases) != 1 || releases[0].Kind != journal.CleanupPanic {
		t.Fatalf("releases = %#v, want the one the source could not perform", releases)
	}
	// Nothing stopped the work, so the release is what the operation ends on.
	var reference *journal.Cause
	if !errors.As(cause, &reference) || reference.Event != releases[0].ID {
		t.Fatalf("cause = %v, want a reference to the release that could not finish", cause)
	}
	if released.Load() != 1 {
		t.Fatalf("release attempts = %d", released.Load())
	}
}
