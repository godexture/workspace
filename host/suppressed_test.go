package host

import (
	"errors"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/errorx"
	"github.com/godexture/godec/internal/journal"
)

type stormFailure struct{ stack []byte }

func (s *stormFailure) Error() string      { return "a payload that could not be released" }
func (s *stormFailure) StackTrace() []byte { return s.stack }

// A run that could not release a hundred thousand payloads reports what
// happened without keeping a hundred thousand copies of it. The count, the
// class, the first and last identity, and the fact that detail was omitted all
// reach the caller through Result rather than only through diagnostics.
func TestAReleaseStormIsReportedByCountRatherThanByCopy(t *testing.T) {
	const occurrences = 100_000
	ledger := journal.NewLedger()
	r := newTestRunner(t.Context(), ledger)
	domain := ledger.Domain("node/keeper", "keeper")
	site := domain.At("keeper")
	failure := &stormFailure{stack: debug.Stack()}
	domain.Perform(journal.Flush, func(*journal.Span) error {
		for range occurrences {
			site.Cleanup(errorx.MarkPanic(failure, failure.stack))
		}
		return nil
	})
	r.collect()

	if len(r.result.Cleanup) == 0 {
		t.Fatal("a storm produced no representative failure")
	}
	if len(r.result.Cleanup) > 8 {
		t.Fatalf("kept %d failures in full, want the run's evidence bounded", len(r.result.Cleanup))
	}
	if len(r.result.Suppressed) != 1 {
		t.Fatalf("suppressed = %#v, want one entry for the class", r.result.Suppressed)
	}
	suppressed := r.result.Suppressed[0]
	if suppressed.Occurrences != occurrences {
		t.Fatalf("occurrences = %d, want %d", suppressed.Occurrences, occurrences)
	}
	if suppressed.Retained != uint64(len(r.result.Cleanup)) {
		t.Fatalf("retained = %d, want the %d entries in Cleanup", suppressed.Retained, len(r.result.Cleanup))
	}
	if suppressed.Omitted() != occurrences-suppressed.Retained {
		t.Fatalf("omitted = %d, want %d", suppressed.Omitted(), occurrences-suppressed.Retained)
	}
	if !suppressed.Truncated {
		t.Fatal("the run did not report that detail was dropped")
	}
	if suppressed.Phase != FlushPhase || suppressed.Task != "node/keeper" || suppressed.Node != "keeper" {
		t.Fatalf("suppressed = %#v, want the site it came from", suppressed)
	}
	// This default budget retained the first representative, so it is searchable
	// through the joined error. Smaller budgets are allowed to retain none.
	if !suppressed.First.Valid() || suppressed.First != r.result.Cleanup[0].ID {
		t.Fatalf("first = %+v, want the retained representative %+v", suppressed.First, r.result.Cleanup[0].ID)
	}
	joined := resultError(r.result)
	var found *stormFailure
	if !errors.As(joined, &found) {
		t.Fatal("the representative failure is not searchable through the joined error")
	}
	// And the count is stated once rather than joined a hundred thousand times.
	if count := strings.Count(joined.Error(), "could not be released"); count > 8 {
		t.Fatalf("the joined error repeats the failure %d times, want the count carried by structure", count)
	}
	if !strings.Contains(joined.Error(), "100000 times") {
		t.Fatalf("the joined error does not state the total: %v", joined)
	}
}

// The ledger is read once, after everything that can write to it has stopped.
// Nothing registered with the run may still be reporting by then: a task that
// had not joined, an operator that had not closed, or a queue that had not been
// discarded would each be able to add evidence after the account was taken.
func TestNothingReportsAfterTheRunCollectsItsEvidence(t *testing.T) {
	state := &lifecycleState{}
	instance, request := lifecycleFixture(t, state)
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := prepared.Run(t.Context())
	if runErr != nil || !result.Succeeded() {
		t.Fatalf("run = %v, result = %#v", runErr, result)
	}
	if err := prepared.Close(); err != nil {
		t.Fatal(err)
	}
	// Every task joined and every operator closed before Run returned, which is
	// what makes the collected Result final rather than a snapshot of a run
	// that could still be writing.
	closed := map[string]int{}
	entries, _ := state.snapshot()
	for _, entry := range entries {
		closed[entry]++
	}
	for _, name := range []string{"close/source", "close/processor", "close/sink"} {
		if closed[name] != 1 {
			t.Fatalf("%s happened %d times before Run returned, want once", name, closed[name])
		}
	}
	if len(result.Cleanup) != 0 || len(result.Secondary) != 0 || len(result.Suppressed) != 0 {
		t.Fatalf("a successful run reported %#v", result)
	}
}

// The evidence a run keeps must not depend on how many times something failed.
func TestRetainedEvidenceDoesNotGrowWithOccurrences(t *testing.T) {
	measure := func(occurrences int) int {
		ledger := journal.NewLedger()
		domain := ledger.Domain("node/keeper", "keeper")
		site := domain.At("keeper")
		failure := &stormFailure{stack: debug.Stack()}
		for range occurrences {
			site.Cleanup(errorx.MarkPanic(failure, failure.stack))
		}
		r := newTestRunner(t.Context(), ledger)
		r.collect()
		return len(r.result.Cleanup) + len(r.result.Suppressed)
	}
	small, large := measure(10), measure(100_000)
	if large != small {
		t.Fatalf("evidence grew from %d entries to %d when occurrences grew ten-thousandfold", small, large)
	}
}

func TestSuppressedPublishesWhenClassCountIsOnlyALowerBound(t *testing.T) {
	ledger := journal.NewBoundedLedger(journal.Budget{Events: 4, GroupSamples: 1, Groups: 1})
	entries := []journal.Entry{
		{Kind: journal.CleanupError, Operation: journal.Run, Task: "a", Node: "one"},
		{Kind: journal.WorkError, Operation: journal.Flush, Task: "b", Node: "two"},
		{Kind: journal.CleanupPanic, Operation: journal.Close, Task: "c", Node: "three"},
	}
	for index, entry := range entries {
		entry.Err = diagnostic.NewError(diagnostic.NewItem("test.class."+string(rune('a'+index)), diagnostic.ErrorSeverity, diagnostic.Path{}, "failed", nil))
		ledger.Record(entry)
	}
	r := newTestRunner(t.Context(), ledger)
	r.collect()
	var found *Suppressed
	for index := range r.result.Suppressed {
		if r.result.Suppressed[index].ClassesTruncated {
			found = &r.result.Suppressed[index]
			break
		}
	}
	if found == nil {
		t.Fatalf("suppressed = %#v, want a lower-bound overflow group", r.result.Suppressed)
	}
	if found.Phase != UnknownPhase {
		t.Fatalf("overflow phase = %q, want %q because operation metadata was discarded", found.Phase, UnknownPhase)
	}
	if !strings.Contains(found.Error(), "at least") {
		t.Fatalf("suppressed rendering presents lower bound as exact: %v", found)
	}
	for _, item := range r.diag.snapshot() {
		if item.Code == "host.suppressed.unknown" && item.Detail["classesTruncated"] == "true" && item.Detail["classes"] != "" {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want classesTruncated", r.diag.snapshot())
}
