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
	generic := Failure{Phase: RunPhase, Task: "runtime/quiesce", Err: cause.Err}
	r.result.Primary = &generic

	if failure := r.acceptTaskReport(report, true); failure != nil {
		t.Fatalf("acceptTaskReport = %v, want nil: cleanup=true routes every non-echoed failure to Result.Cleanup", failure)
	}
	if len(r.result.Cleanup) != 1 {
		t.Fatalf("cleanup = %#v, want exactly one: the independent failure sharing the primary's sentinel error must not be dropped", r.result.Cleanup)
	}
}
