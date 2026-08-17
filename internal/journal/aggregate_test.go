package journal

import (
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/errorx"
	"testing"
)

// storm is a release failure carrying a stack, which is what a declared Drop
// that raises produces.
type storm struct {
	message string
	stack   []byte
}

func (s *storm) Error() string      { return s.message }
func (s *storm) StackTrace() []byte { return s.stack }

func newStorm(message string) *storm { return &storm{message: message, stack: debug.Stack()} }

func markedStorm(value *storm) error { return errorx.MarkPanic(value, value.stack) }

func groupsOf(ledger *Ledger) map[string]Group {
	result := make(map[string]Group)
	for _, group := range ledger.Groups() {
		result[group.Class.Failure] = group
	}
	return result
}

// A hundred thousand payloads that could not be released is one class, one
// count, a bounded number of samples, and an explicit statement that the rest
// were omitted. Every release is still attempted and still counted; what the
// budget bounds is how many full copies the run keeps.
func TestARepeatedReleaseFailureIsCountedNotCopied(t *testing.T) {
	const occurrences = 100_000
	budget := DefaultBudget()
	ledger := NewBoundedLedger(budget)
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	failure := newStorm("a payload that could not be released")
	attempted := 0
	domain.Perform(Flush, func(*Span) error {
		for range occurrences {
			// The reporting loop is never stopped by the budget: every payload
			// is released and every release is accounted for.
			attempted++
			site.Cleanup(markedStorm(failure))
		}
		return nil
	})

	if attempted != occurrences {
		t.Fatalf("attempted %d releases, want %d: a budget must not stop the cleanup loop", attempted, occurrences)
	}
	if got := ledger.Occurrences(); got != occurrences {
		t.Fatalf("occurrences = %d, want every one counted", got)
	}
	groups := ledger.Groups()
	if len(groups) != 1 {
		t.Fatalf("groups = %#v, want one class", groups)
	}
	group := groups[0]
	if group.Count != occurrences {
		t.Fatalf("group count = %d, want %d", group.Count, occurrences)
	}
	if len(group.Samples) != budget.GroupSamples {
		t.Fatalf("samples = %d, want the budget's %d", len(group.Samples), budget.GroupSamples)
	}
	if group.Omitted != occurrences-uint64(len(group.Samples)) {
		t.Fatalf("omitted = %d, want %d", group.Omitted, occurrences-uint64(len(group.Samples)))
	}
	if !group.Truncated {
		t.Fatal("the group did not say its detail was truncated")
	}
	// What the run keeps does not scale with what happened to it.
	if events := ledger.Events(); len(events) > budget.Events {
		t.Fatalf("retained %d events, want at most the budget's %d", len(events), budget.Events)
	}
	if first := group.First; !first.Valid() || first.Seq != 1 {
		t.Fatalf("first = %+v, want the earliest occurrence", first)
	}
	if group.Last.Seq != occurrences {
		t.Fatalf("last = %+v, want the final occurrence", group.Last)
	}
}

// One stack repeated is stored once. A group's samples share the interned
// bytes rather than each keeping a copy.
func TestOneCallSiteIsStoredOnce(t *testing.T) {
	ledger := NewBoundedLedger(Budget{Events: 8, GroupSamples: 8, Groups: 8, Stacks: 4, StackBytes: 1 << 20})
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	failure := newStorm("one call site")
	for range 8 {
		site.Cleanup(markedStorm(failure))
	}
	events := ledger.Events()
	if len(events) < 2 {
		t.Fatalf("events = %d, want several samples to compare", len(events))
	}
	first := events[0].Stack
	for _, event := range events[1:] {
		if len(event.Stack) == 0 || &event.Stack[0] != &first[0] {
			t.Fatal("two failures from one call site kept separate copies of the stack")
		}
	}
	if ledger.depot.bytes != len(failure.stack) {
		t.Fatalf("depot holds %d bytes, want one copy of %d", ledger.depot.bytes, len(failure.stack))
	}
}

// A declared Drop that raises captures a fresh stack every time. Two captures
// from one call site are equal, so the depot stores one and the class key stays
// stable across the whole storm -- which is what makes a storm one class rather
// than one class per occurrence.
func TestStacksCapturedSeparatelyAtOneCallSiteInternOnce(t *testing.T) {
	ledger := NewBoundedLedger(Budget{Events: 8, GroupSamples: 8, Groups: 8, Stacks: 8, StackBytes: 1 << 20})
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	for range 8 {
		value := &storm{message: "raised again", stack: debug.Stack()}
		site.Cleanup(markedStorm(value))
	}
	if got := len(ledger.depot.stacks); got != 1 {
		t.Fatalf("depot holds %d stacks, want one for one call site", got)
	}
	if groups := ledger.Groups(); len(groups) != 1 {
		t.Fatalf("groups = %#v, want one class for one call site", groups)
	}
}

// A stack the budget refuses is a different thing from a failure that never had
// one, and the difference is recorded rather than lost.
func TestAStackDroppedByBudgetIsDistinguishedFromNoStack(t *testing.T) {
	ledger := NewBoundedLedger(Budget{Events: 8, GroupSamples: 8, Groups: 8, Stacks: 0, StackBytes: 0})
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	site.Cleanup(markedStorm(newStorm("carries a stack the budget refuses")))
	site.Cleanup(errors.New("never had a stack"))

	groups := groupsOf(ledger)
	if len(groups) != 2 {
		t.Fatalf("groups = %#v, want one per class", groups)
	}
	var withStack, without Group
	for _, group := range groups {
		if group.Class.Kind == CleanupPanic {
			withStack = group
		} else {
			without = group
		}
	}
	if !withStack.Truncated {
		t.Fatal("a stack dropped by budget was not reported as truncated")
	}
	if without.Truncated {
		t.Fatal("a failure that never had a stack was reported as truncated")
	}
}

// An error whose class varies without bound must grow neither the event list
// nor the class table. Beyond the budget, classes fold into one coarse group
// that still carries the total and says how many distinctions it lost.
func TestManyDistinctClassesFoldIntoABoundedOverflowGroup(t *testing.T) {
	const classes = 500
	budget := Budget{Events: 4, GroupSamples: 1, Groups: 4, Stacks: 8, StackBytes: 1 << 20}
	ledger := NewBoundedLedger(budget)
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	for index := range classes {
		// A distinct Go type per class is the worst case a fingerprint built
		// from structure can face.
		site.Cleanup(distinctClass(index))
	}
	groups := ledger.Groups()
	if len(groups) > budget.Groups+1 {
		t.Fatalf("groups = %d, want the class table bounded by the budget", len(groups))
	}
	var total uint64
	var overflow *Group
	for index := range groups {
		total = total + groups[index].Count
		if groups[index].Overflow() {
			overflow = &groups[index]
		}
	}
	if total != classes {
		t.Fatalf("counted %d occurrences, want %d", total, classes)
	}
	if overflow == nil {
		t.Fatal("distinct classes beyond the budget were dropped instead of folded")
	}
	if !overflow.Truncated {
		t.Fatal("the overflow group did not report that distinctions were lost")
	}
	if overflow.Classes < 2 {
		t.Fatalf("overflow folded %d classes, want the ones the budget refused to track", overflow.Classes)
	}
	if ledger.Occurrences() != classes {
		t.Fatalf("occurrences = %d, want %d", ledger.Occurrences(), classes)
	}
}

// distinctClass returns an error carrying a diagnostic code that differs on
// every occurrence. A code is the highest-cardinality thing the class key
// legitimately reads, so this is the worst case the class table can face.
func distinctClass(index int) error {
	return diagnostic.NewError(diagnostic.NewItem(
		fmt.Sprintf("release.class-%d", index),
		diagnostic.ErrorSeverity,
		diagnostic.Path{},
		"a payload that could not be released",
		nil,
	))
}

// The class key is built from structure. An error whose message differs on
// every occurrence must not escape aggregation by varying its text, and the
// text must not become part of what the ledger stores as a key.
func TestAVaryingMessageDoesNotEscapeAggregation(t *testing.T) {
	const occurrences = 1000
	ledger := NewBoundedLedger(DefaultBudget())
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	for index := range occurrences {
		site.Cleanup(fmt.Errorf("payload %d could not be released", index))
	}
	groups := ledger.Groups()
	if len(groups) != 1 {
		t.Fatalf("groups = %#v, want one class despite a message per occurrence", groups)
	}
	if groups[0].Count != occurrences {
		t.Fatalf("count = %d, want %d", groups[0].Count, occurrences)
	}
	if strings.Contains(groups[0].Class.Failure, "payload 0") {
		t.Fatalf("the class key = %q, want structure rather than what the error says", groups[0].Class.Failure)
	}
}

// A cancellation cause carries its own error, so aggregation need not reserve
// the first representative event merely to make a later echo recognizable.
func TestACancellationCauseSurvivesAStorm(t *testing.T) {
	ledger := NewBoundedLedger(Budget{Events: 1, GroupSamples: 1, Groups: 2, Stacks: 2, StackBytes: 1 << 20})
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	release := newStorm("the release that stopped the run")
	cause := domain.Perform(Run, func(*Span) error {
		for range 10_000 {
			site.Cleanup(release)
		}
		return nil
	})
	var reference *Cause
	if !errors.As(cause, &reference) {
		t.Fatalf("cause = %v, want a reference to the first occurrence", cause)
	}
	if reference.Err == nil {
		t.Fatal("cause lost its self-contained error")
	}
	// And re-observing it still resolves rather than recording again.
	before := ledger.Occurrences()
	if again := ledger.Record(Entry{Kind: WorkError, Operation: Run, Err: cause}); again == nil || again.ID != reference.Event {
		t.Fatalf("re-observation = %#v, want the event it names", again)
	}
	if ledger.Occurrences() != before {
		t.Fatal("re-observing an event counted it again")
	}
}

// A storm of releases never displaces what stopped the work, and nothing the
// panicking code chose appears in a sample, a class key, or a rendering.
func TestAStormNeverDisplacesThePrimaryOrLeaksAValue(t *testing.T) {
	const secret = "aggregate-panic-secret"
	type credential struct{ Token string }
	ledger := NewBoundedLedger(Budget{Events: 2, GroupSamples: 1, Groups: 4, Stacks: 4, StackBytes: 1 << 20})
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	stopped := errors.New("what stopped the work")
	domain.Perform(Flush, func(*Span) error {
		for range 5000 {
			site.Cleanup(&PanicError{Name: "keeper", Summary: recoveredSummary(credential{Token: secret}), Stack: debug.Stack()})
		}
		return stopped
	})

	work := 0
	for _, event := range ledger.Events() {
		if !event.Kind.Cleanup() {
			work++
			if !errors.Is(event.Err, stopped) {
				t.Fatalf("work failure = %v, want the one that stopped it", event.Err)
			}
		}
		if strings.Contains(event.Error(), secret) {
			t.Error("a retained sample renders the value the panicking code chose")
		}
	}
	if work != 1 {
		t.Fatalf("work failures kept = %d, want the one that stopped the run", work)
	}
	for _, group := range ledger.Groups() {
		if strings.Contains(group.Class.Failure, secret) {
			t.Error("a class key carries the value the panicking code chose")
		}
	}
}

// Reporting is safe from every goroutine that can hold a slot. No occurrence is
// lost, none is counted twice, and no two share an identity.
func TestConcurrentStormKeepsAnExactCount(t *testing.T) {
	const reporters, each = 8, 2000
	ledger := NewBoundedLedger(DefaultBudget())
	failure := newStorm("concurrent release failure")
	var wait sync.WaitGroup
	wait.Add(reporters)
	for range reporters {
		go func() {
			defer wait.Done()
			domain := ledger.Domain("keeper", "node")
			site := domain.At("node")
			for range each {
				site.Cleanup(markedStorm(failure))
			}
		}()
	}
	wait.Wait()

	if got := ledger.Occurrences(); got != reporters*each {
		t.Fatalf("occurrences = %d, want %d", got, reporters*each)
	}
	var total uint64
	for _, group := range ledger.Groups() {
		total += group.Count
	}
	if total != reporters*each {
		t.Fatalf("counted %d across groups, want %d", total, reporters*each)
	}
	seen := make(map[EventID]struct{})
	for _, event := range ledger.Events() {
		if _, exists := seen[event.ID]; exists {
			t.Fatalf("identity %+v was issued twice", event.ID)
		}
		seen[event.ID] = struct{}{}
	}
}

// The smallest budget there is must still report what happened. Keeping
// nothing in full is a legitimate setting; keeping no count is not.
func TestTheSmallestBudgetStillReportsWhatHappened(t *testing.T) {
	ledger := NewBoundedLedger(Budget{})
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	for range 100 {
		site.Cleanup(markedStorm(newStorm("nothing may be retained")))
	}
	if got := ledger.Occurrences(); got != 100 {
		t.Fatalf("occurrences = %d, want every one counted", got)
	}
	groups := ledger.Groups()
	if len(groups) != 1 {
		t.Fatalf("groups = %#v, want the class to exist even with no room for samples", groups)
	}
	if groups[0].Count != 100 || !groups[0].Truncated {
		t.Fatalf("group = %#v, want the count and the truncation", groups[0])
	}
	if groups[0].Omitted == 0 {
		t.Fatal("nothing was reported as omitted although nothing was retained")
	}
	// Work samples are bounded too. The stop provenance remains separately
	// available to explain the run.
	domain.Perform(Run, func(*Span) error { return errors.New("what stopped the work") })
	work := 0
	for _, event := range ledger.Events() {
		if !event.Kind.Cleanup() {
			work++
		}
	}
	if work != 0 {
		t.Fatalf("work samples kept = %d, want none under the zero budget", work)
	}
	if stopped, ok := ledger.Stopping(); !ok || stopped.Kind != WorkError {
		t.Fatalf("stopping = %#v, want the work provenance", stopped)
	}
}

// Counting saturates rather than wrapping. A count that wrapped would
// understate what happened, which is the one thing this design exists to
// prevent.
func TestCountsSaturateRatherThanWrap(t *testing.T) {
	if got := add(^uint64(0)-1, 5); got != ^uint64(0) {
		t.Fatalf("saturating add = %d, want the maximum", got)
	}
}

func BenchmarkCleanupStormRecording(b *testing.B) {
	ledger := NewBoundedLedger(DefaultBudget())
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	failure := newStorm("a payload that could not be released")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		site.Cleanup(markedStorm(failure))
	}
	b.StopTimer()
	// What the run keeps must not grow with what happened to it.
	b.ReportMetric(float64(len(ledger.Events())), "retained")
	b.ReportMetric(float64(ledger.depot.bytes), "stack-bytes")
}
