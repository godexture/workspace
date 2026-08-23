package host

import (
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/task"
)

func newTestRunner(ctx context.Context, ledger *journal.Ledger) *runner {
	return &runner{ctx: ctx, ledger: ledger, diag: &diagnosticLog{}}
}

// Two independent tasks that happen to fail with the exact same sentinel error
// are two failures. Nothing here compares what an error says or what its chain
// contains: identity comes from the ledger, so resemblance cannot make one
// failure disappear into another.
func TestIndependentFailuresWithOneSentinelStayTwoFailures(t *testing.T) {
	sentinel := errors.New("boom")
	ledger := journal.NewLedger()
	group := task.New(context.Background(), ledger)
	for _, name := range []string{"task-a", "task-b"} {
		if err := group.StartDomain(ledger.Domain(name, name), func(context.Context, *journal.Span) error {
			return sentinel
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	group.Wait(context.Background())

	events := ledger.Events()
	if len(events) != 2 {
		t.Fatalf("events = %#v, want one per task", events)
	}
	if events[0].ID == events[1].ID {
		t.Fatal("two independent failures share one identity")
	}

	r := newTestRunner(context.Background(), ledger)
	r.collect()
	if r.result.Primary == nil || len(r.result.Secondary) != 1 || len(r.result.Cleanup) != 0 {
		t.Fatalf("result = %#v, want one primary and one independent secondary", r.result)
	}
	if r.result.Primary.ID == r.result.Secondary[0].ID {
		t.Fatal("the same event was reported twice")
	}
}

// A peer that observes the cancellation and returns context.Cause(ctx)
// verbatim -- the pattern this codebase uses everywhere -- is talking about the
// event that stopped the run, so it adds nothing. The suppression is by
// identity, not by the error resembling the one already recorded.
func TestAPeerPropagatingTheCauseIsNotASecondFailure(t *testing.T) {
	ledger := journal.NewLedger()
	group := task.New(context.Background(), ledger)
	stopped := make(chan struct{})
	if err := group.StartDomain(ledger.Domain("peer", "peer"), func(ctx context.Context, _ *journal.Span) error {
		<-ctx.Done()
		close(stopped)
		return context.Cause(ctx)
	}, nil); err != nil {
		t.Fatal(err)
	}
	want := errors.New("the failure that stopped the run")
	if err := group.StartDomain(ledger.Domain("failing", "failing"), func(context.Context, *journal.Span) error {
		return want
	}, nil); err != nil {
		t.Fatal(err)
	}
	group.Wait(context.Background())
	<-stopped

	events := ledger.Events()
	if len(events) != 1 || !errors.Is(events[0].Err, want) {
		t.Fatalf("events = %#v, want only the failure that stopped the run", events)
	}
}

// The same event observed again is the same event, however many boundaries
// look at it. A run reads its cancellation cause at every step it reaches.
func TestReobservingOneEventNeverRecordsItTwice(t *testing.T) {
	ledger := journal.NewLedger()
	r := newTestRunner(context.Background(), ledger)
	first := r.record(journal.WorkError, journal.Run, "node", "task", errors.New("the original failure"))
	if first == nil {
		t.Fatal("the failure was not recorded")
	}
	cause := ledger.Stopped()
	if cause == nil {
		t.Fatal("the ledger produced no cause for the failure that stopped it")
	}
	for range 3 {
		again := r.record(journal.WorkError, journal.Flush, "", "runtime/finish", cause)
		if again == nil || again.ID != first.ID {
			t.Fatalf("re-observation = %#v, want the event it names", again)
		}
	}
	if events := ledger.Events(); len(events) != 1 {
		t.Fatalf("events = %#v, want one", events)
	}
}

// Two failures that read identically but are different events stay two. This is
// the same input the echo check used to fold together by comparing error
// content.
func TestOneMessageWithTwoIdentitiesStaysTwoFailures(t *testing.T) {
	ledger := journal.NewLedger()
	r := newTestRunner(context.Background(), ledger)
	first := r.record(journal.WorkError, journal.Run, "a", "task-a", errors.New("identical text"))
	second := r.record(journal.WorkError, journal.Run, "b", "task-b", errors.New("identical text"))
	if first.ID == second.ID {
		t.Fatal("two distinct failures share one identity")
	}
	if events := ledger.Events(); len(events) != 2 {
		t.Fatalf("events = %#v, want both", events)
	}
}

// The third of the three defects.
//
// A direct chain's own Flush failure and a buffered task's failure are
// independent: neither is the other's echo, and the second happens to be what
// cancelled the run. The old check asked "is something already Primary, and
// does the run's cancellation cause name this event?" -- which is true here for
// the wrong reason, so the buffered task's failure vanished from the Result
// entirely. Nothing resolves an event against the run's cancellation any more;
// an event is suppressed only when the error being offered is that event.
func TestADirectFlushFailureAndTheCancellingTaskFailureBothSurvive(t *testing.T) {
	ledger := journal.NewLedger()
	r := newTestRunner(context.Background(), ledger)

	// The buffered task fails first and its cause is what cancels the run.
	buffered := ledger.Domain("buffer/edge", "edge")
	bufferedFailure := errors.New("the buffered task's own failure")
	cause := buffered.Perform(journal.Run, func(*journal.Span) error { return bufferedFailure })
	if cause == nil {
		t.Fatal("the buffered task produced no cause")
	}
	ctx, stop := context.WithCancelCause(context.Background())
	stop(cause)
	r.ctx = ctx

	// The direct chain's own Flush fails independently, discovered through
	// Execution.Finish rather than through the cancellation.
	directFailure := errors.New("the direct chain's own flush failure")
	source := ledger.Domain("source/source", "source")
	source.Perform(journal.Flush, func(*journal.Span) error { return directFailure })

	r.collect()
	found := map[string]int{}
	for _, failure := range append(append([]Failure{*r.result.Primary}, r.result.Secondary...), r.result.Cleanup...) {
		found[failure.Err.Error()]++
	}
	if found[bufferedFailure.Error()] != 1 {
		t.Fatalf("the failure that cancelled the run appears %d times, want once: %#v", found[bufferedFailure.Error()], r.result)
	}
	if found[directFailure.Error()] != 1 {
		t.Fatalf("the direct chain's flush failure appears %d times, want once: %#v", found[directFailure.Error()], r.result)
	}
	// The one that stopped the run is what the run is reported as having
	// stopped for; the other is independent, not cleanup and not a diagnostic.
	if !errors.Is(r.result.Primary.Err, bufferedFailure) {
		t.Fatalf("primary = %v, want the failure that stopped the run", r.result.Primary)
	}
	if len(r.result.Secondary) != 1 || !errors.Is(r.result.Secondary[0].Err, directFailure) {
		t.Fatalf("secondary = %#v, want the independent flush failure", r.result.Secondary)
	}
	if len(r.result.Cleanup) != 0 {
		t.Fatalf("cleanup = %#v, want none: neither failure was a release", r.result.Cleanup)
	}
}

// A run can be stopped by a release nobody could perform, with nothing having
// failed at its work at all. That cause still names a real event, and the event
// stays cleanup: it is not what stopped useful work, because no useful work
// stopped.
func TestACleanupOnlyFailureCanBeTheCancellationCause(t *testing.T) {
	ledger := journal.NewLedger()
	domain := ledger.Domain("source/source", "source")
	release := errors.New("a payload that could not be released")
	cause := domain.Perform(journal.Run, func(*journal.Span) error {
		domain.At("source").Cleanup(release)
		return nil
	})
	if cause == nil {
		t.Fatal("an operation ending on an unreleased payload produced no cause")
	}
	var reference *journal.Cause
	if !errors.As(cause, &reference) {
		t.Fatalf("cause = %v, want a reference to the event", cause)
	}
	if event, ok := ledger.Event(reference.Event); !ok || !errors.Is(event.Err, release) {
		t.Fatalf("cause names %+v, which is not the release", reference.Event)
	}

	ctx, stop := context.WithCancelCause(context.Background())
	stop(cause)
	r := newTestRunner(ctx, ledger)
	// Whatever notices the cancellation next re-observes that event.
	r.record(journal.WorkError, journal.Run, "", "runtime/quiesce", context.Cause(ctx))
	r.collect()
	if r.result.Primary != nil || len(r.result.Secondary) != 0 {
		t.Fatalf("result = %#v, want no work failure: only a release failed", r.result)
	}
	if len(r.result.Cleanup) != 1 || !errors.Is(r.result.Cleanup[0].Err, release) {
		t.Fatalf("cleanup = %#v, want the one release that could not finish", r.result.Cleanup)
	}
}

// Identity must survive everything a run can do to a task: two tasks sharing a
// display name, two task groups, one task's Run and Flush and Close, several
// events in one operation, repeated operations, and concurrent recording.
func TestEventIdentityNeverCollides(t *testing.T) {
	ledger := journal.NewLedger()
	seen := map[journal.EventID]struct{}{}
	sameName := errors.New("shared")

	// Two groups, and two tasks sharing one display name inside each.
	for range 2 {
		group := task.New(context.Background(), ledger)
		for range 2 {
			if err := group.StartDomain(ledger.Domain("worker", "node"), func(context.Context, *journal.Span) error {
				return sameName
			}, nil); err != nil {
				t.Fatal(err)
			}
		}
		group.Wait(context.Background())
	}

	// One task's Run, Flush and Close, with two events inside one operation and
	// the same operation performed twice.
	domain := ledger.Domain("worker", "node")
	for _, operation := range []journal.Operation{journal.Run, journal.Flush, journal.Close, journal.Flush} {
		domain.Perform(operation, func(*journal.Span) error {
			domain.At("node").Cleanup(sameName)
			domain.At("node").Cleanup(sameName)
			return sameName
		})
	}

	// Concurrent recording from several domains at once.
	done := make(chan struct{})
	for index := range 4 {
		go func(index int) {
			defer func() { done <- struct{}{} }()
			concurrent := ledger.Domain("worker", "node")
			for range 8 {
				concurrent.At("node").Cleanup(sameName)
			}
			_ = index
		}(index)
	}
	for range 4 {
		<-done
	}

	events := ledger.Events()
	for _, event := range events {
		if !event.ID.Valid() {
			t.Fatalf("event %#v has no identity", event)
		}
		if _, exists := seen[event.ID]; exists {
			t.Fatalf("identity %+v was issued twice", event.ID)
		}
		seen[event.ID] = struct{}{}
	}
	if got := ledger.Occurrences(); got < 4+12+32 {
		t.Fatalf("occurrences = %d, want every failure counted", got)
	}
}

// A run's identities never collide with another run's, so a cause that escapes
// one run cannot resolve inside another.
func TestOneRunsIdentityDoesNotResolveInAnother(t *testing.T) {
	first := journal.NewLedger()
	second := journal.NewLedger()
	failure := first.Record(journal.Entry{Kind: journal.WorkError, Operation: journal.Run, Err: errors.New("first run")})
	if _, ok := second.Event(failure.ID); ok {
		t.Fatal("another run resolved an identity that is not its own")
	}
	echoed := second.Record(journal.Entry{Kind: journal.WorkError, Operation: journal.Run, Err: &journal.Cause{Event: failure.ID, Err: errors.New("first run")}})
	if echoed.ID == failure.ID {
		t.Fatal("a foreign identity was adopted rather than recorded as this run's own failure")
	}
}

// Joining reports what only joining can know. The failures the tasks produced
// are already in the ledger, so this adds facts rather than repeating them.
func TestJoinReportsUnstoppedTasksAsCleanupDuringCleanup(t *testing.T) {
	ledger := journal.NewLedger()
	r := newTestRunner(context.Background(), ledger)
	r.acceptTaskReport(task.Report{Running: []string{"stuck"}, WaitErr: context.DeadlineExceeded}, true)
	r.collect()
	if r.result.Primary != nil || len(r.result.Secondary) != 0 {
		t.Fatalf("result = %#v, want nothing claiming to have stopped the run", r.result)
	}
	if len(r.result.Cleanup) != 2 {
		t.Fatalf("cleanup = %#v, want the unstopped task and the wait that timed out", r.result.Cleanup)
	}
	for _, failure := range r.result.Cleanup {
		if failure.Phase != JoinPhase {
			t.Errorf("phase = %v, want join", failure.Phase)
		}
	}
}

func TestOmittedCauseEchoKeepsOriginalAttributionForHostProjection(t *testing.T) {
	ledger := journal.NewBoundedLedger(journal.Budget{})
	domain := ledger.Domain("cleanup-task", "home")
	cleanupCause := domain.Perform(journal.Close, func(*journal.Span) error {
		domain.At("declared-node").Cleanup(errors.New("release failed"))
		return nil
	})
	domain.Perform(journal.Run, func(*journal.Span) error { return errors.New("work failed") })
	before := ledger.Occurrences()

	runner := newTestRunner(context.Background(), ledger)
	failure := runner.record(journal.WorkError, journal.Flush, "observed-node", "flush", cleanupCause)
	if failure == nil {
		t.Fatal("omitted cause echo produced no projected failure")
	}
	if got := ledger.Occurrences(); got != before {
		t.Fatalf("echo occurrences = %d, want unchanged at %d", got, before)
	}
	if failure.Phase != ClosePhase || failure.Task != "cleanup-task" || failure.Node != "declared-node" {
		t.Fatalf("projected echo = %+v, want close/cleanup-task/declared-node", failure)
	}
}

func TestLifecyclePhaseOperationMappingIsBidirectional(t *testing.T) {
	if len(phaseOperations) != 13 {
		t.Fatalf("mapping entries = %d, want all 13 lifecycle operations", len(phaseOperations))
	}
	for _, pair := range phaseOperations {
		if got := phaseOf(pair.operation); got != pair.phase {
			t.Errorf("phaseOf(%v) = %q, want %q", pair.operation, got, pair.phase)
		}
		if got := operationOf(pair.phase); got != pair.operation {
			t.Errorf("operationOf(%q) = %v, want %v", pair.phase, got, pair.operation)
		}
	}
}

func TestLifecyclePhaseOperationMappingRejectsUnknownValues(t *testing.T) {
	assertPanics := func(t *testing.T, call func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Error("unknown lifecycle value did not panic")
			}
		}()
		call()
	}
	assertPanics(t, func() { phaseOf(journal.Operation(0)) })
	assertPanics(t, func() { phaseOf(journal.Operation(255)) })
	assertPanics(t, func() { operationOf(Phase("")) })
	assertPanics(t, func() { operationOf(Phase("future")) })
}
