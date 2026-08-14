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
	scope := journal.NewScope("sink")
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
	scope := journal.NewScope("sink")
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

// Sealing takes an attempt's result, not the journal. A later lifecycle
// operation runs over the same slots, and what it cannot release belongs to
// that operation rather than being discarded for arriving after a result was
// taken.
func TestSealEndsAnAttemptRatherThanTheJournal(t *testing.T) {
	scope := journal.NewScope("node")
	scope.Cleanup(errors.New("during the run"))
	first := scope.Seal()
	if len(first.Cleanup) != 1 {
		t.Fatalf("first attempt = %#v", first.Cleanup)
	}
	scope.Cleanup(errors.New("during the flush"))
	second := scope.Seal()
	if len(second.Cleanup) != 1 {
		t.Fatalf("a failure reported after the seal was discarded: %#v", second.Cleanup)
	}
	if first.Cleanup[0].ID.Attempt == second.Cleanup[0].ID.Attempt {
		t.Fatal("both attempts share an identity, so a consumer cannot tell them apart")
	}
}

// Two releases that failed the same way in the same place are two payloads that
// were not released. An identity keeps them apart where their text cannot.
func TestIndependentFailuresKeepSeparateIdentities(t *testing.T) {
	scope := journal.NewScope("node")
	same := errors.New("the same release failure")
	scope.Cleanup(same)
	scope.Cleanup(same)
	outcome := scope.Seal()
	if len(outcome.Cleanup) != 2 {
		t.Fatalf("cleanup = %#v, want both events", outcome.Cleanup)
	}
	first, second := outcome.Cleanup[0].ID, outcome.Cleanup[1].ID
	if first.Attempt != second.Attempt || first.Seq == second.Seq {
		t.Fatalf("identities = %+v and %+v, want one attempt and two events", first, second)
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
		scope := journal.NewScope("node")
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
