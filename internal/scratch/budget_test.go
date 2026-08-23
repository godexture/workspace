package scratch

import (
	"errors"
	"testing"
)

// A growing store sets nothing aside, so what stops it is the running total of
// what every such store has written. Two of them therefore run out together
// rather than each on its own.
func TestGrowingJournalsShareOneRunningTotal(t *testing.T) {
	budget := NewBudget(24, false)
	first, err := OpenGrowing(budget, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenGrowing(budget, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	if _, err := first.Append(t.Context(), make([]byte, 16)); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if _, err := second.Append(t.Context(), make([]byte, 8)); err != nil {
		t.Fatalf("second append: %v", err)
	}
	if got := budget.Used(); got != 24 {
		t.Fatalf("used = %d, want 24", got)
	}
	if _, err := second.Append(t.Context(), []byte{0}); !errors.Is(err, ErrQuota) {
		t.Fatalf("append past the shared ceiling = %v", err)
	}
}

// Closing one gives its bytes back, because the ceiling is on what is being
// held at once rather than on what a job has written over its life.
func TestClosingAGrowingJournalReturnsWhatItHeld(t *testing.T) {
	budget := NewBudget(16, false)
	journal, err := OpenGrowing(budget, 64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(t.Context(), make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if got := budget.Used(); got != 0 {
		t.Fatalf("used after close = %d, want 0", got)
	}
	next, err := OpenGrowing(budget, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err := next.Append(t.Context(), make([]byte, 16)); err != nil {
		t.Fatalf("reusing the returned bytes: %v", err)
	}
}

// A node's own ceiling still holds even when the job lifted its. The two bound
// different things: one is what this component promised, the other is what the
// job will tolerate altogether.
func TestAGrowingJournalKeepsItsOwnCeilingUnderAnUnlimitedJob(t *testing.T) {
	budget := NewBudget(0, true)
	journal, err := OpenGrowing(budget, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if _, err := journal.Append(t.Context(), make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Append(t.Context(), []byte{0}); !errors.Is(err, ErrQuota) {
		t.Fatalf("append past the component ceiling = %v", err)
	}
	if got := budget.Used(); got != 8 {
		t.Fatalf("an unlimited job still counts what is held: used = %d", got)
	}
}

// A job that left no ceiling and did not lift it has no growing stores at all,
// which is refused when one is asked for rather than when it is first written.
func TestADisabledBudgetOpensNoGrowingJournal(t *testing.T) {
	if _, err := OpenGrowing(NewBudget(0, false), 64); !errors.Is(err, ErrDisabled) {
		t.Fatalf("opening under a disabled budget = %v", err)
	}
	if _, err := OpenGrowing(nil, 64); !errors.Is(err, ErrDisabled) {
		t.Fatalf("opening without a budget = %v", err)
	}
}
