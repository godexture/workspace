package host

import (
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/task"
)

// Two independent tasks that happen to fail with the exact same sentinel
// error must both be reported. The echo check that recognizes a data task's
// own journal outcome as the same event already committed to Result.Primary
// compares EventID, not error content, so a second task's genuinely
// independent failure -- which is not what stopped the run -- is never
// mistaken for a repeat sighting just because its error reads the same.
func TestIndependentFailuresWithTheSameSentinelAreNotEchoSuppressed(t *testing.T) {
	sentinel := errors.New("boom")
	group := task.New(context.Background())
	if err := group.StartScoped("task-a", journal.New(journal.Run, "a"), func(context.Context) error { return sentinel }); err != nil {
		t.Fatal(err)
	}
	if err := group.StartScoped("task-b", journal.New(journal.Run, "b"), func(context.Context) error { return sentinel }); err != nil {
		t.Fatal(err)
	}
	report := group.Wait(context.Background())
	if len(report.Outcomes) != 2 {
		t.Fatalf("outcomes = %d, want one per task", len(report.Outcomes))
	}

	// Whichever task's failure actually won the group's cancellation race is
	// the one Host would have discovered first, through a generically
	// attributed catch like Quiesce's own; which one wins does not matter.
	var cause *journal.Cause
	if !errors.As(context.Cause(group.Context()), &cause) {
		t.Fatal("the group did not record a journal.Cause")
	}
	r := &runner{ctx: group.Context(), reported: make(map[string]struct{}), diag: &diagnosticLog{}}
	// Production wraps context.Cause(ctx) itself into Failure.Err (failureOf
	// never unwraps it), so the mock must keep the *journal.Cause wrapper too:
	// the echo check reads it from Primary's own chain, not from context.Cause
	// again.
	generic := Failure{Phase: RunPhase, Task: "runtime/quiesce", Err: cause}
	r.result.Primary = &generic

	if failure := r.acceptTaskReport(report, true); failure != nil {
		t.Fatalf("acceptTaskReport = %v, want nil: cleanup=true routes every non-echoed failure to Result.Cleanup", failure)
	}
	if len(r.result.Cleanup) != 1 {
		t.Fatalf("cleanup = %#v, want exactly one: the independent failure sharing the primary's sentinel error must not be dropped", r.result.Cleanup)
	}
}

// The echo check exists to recognize a task's own failure as the same event
// that already became Result.Primary through a generic cancellation catch
// like Quiesce's own. It must not fire just because *something* is already
// Primary: a direct chain's own Flush failure, discovered through
// Execution.Finish rather than through context cancellation, has nothing to
// do with whatever independently cancelled the run. Comparing the failure's
// EventID against context.Cause(ctx) alone -- without checking that Primary
// itself came from that cause -- would drop the genuinely different failure
// that actually caused the cancellation, with no trace anywhere in Result.
func TestAnUnrelatedPrimaryDoesNotSuppressTheFailureThatActuallyCausedCancellation(t *testing.T) {
	scope := journal.New(journal.Run, "buffer")
	scope.Attach(1, "buffer")
	scope.Fail(errors.New("the buffered task's own failure"))
	outcome := scope.Seal()
	cause := outcome.Cause()
	if cause == nil {
		t.Fatal("the outcome did not produce a cancellation cause")
	}

	ctx, stop := context.WithCancelCause(context.Background())
	stop(cause)

	r := &runner{ctx: ctx, reported: make(map[string]struct{}), diag: &diagnosticLog{}}
	direct := Failure{Phase: FlushPhase, Task: "runtime/finish", Err: errors.New("the direct chain's own flush failure")}
	r.result.Primary = &direct

	if failure := r.acceptTaskReport(task.Report{Outcomes: []journal.Outcome{outcome}}, true); failure != nil {
		t.Fatalf("acceptTaskReport = %v, want nil: cleanup=true routes every non-echoed failure to Result.Cleanup", failure)
	}
	if len(r.result.Cleanup) != 1 {
		t.Fatalf("cleanup = %#v, want exactly one: the failure that actually caused the cancellation must not be dropped just because an unrelated Primary is already set", r.result.Cleanup)
	}
}
