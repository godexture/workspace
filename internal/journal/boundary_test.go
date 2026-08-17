package journal

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// blocking is an error whose chain walk can be held open, which is what puts a
// report in flight for as long as a test needs.
type blocking struct {
	inner   error
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type panickingUnwrap struct{ err error }

func (p panickingUnwrap) Error() string { return p.err.Error() }
func (panickingUnwrap) Unwrap() error   { panic("plugin unwrap panic") }

func (b *blocking) Error() string { return b.inner.Error() }

// Unwrap is the third-party code the ledger walks outside its lock. Blocking
// here is what opens the window a span boundary could otherwise slip through.
func (b *blocking) Unwrap() error {
	b.once.Do(func() {
		close(b.entered)
		<-b.release
	})
	return b.inner
}

func newBlocking(message string) *blocking {
	return &blocking{inner: errors.New(message), entered: make(chan struct{}), release: make(chan struct{})}
}

// A report belongs to the span that was open when it started, not to whichever
// span happens to be open when it finishes.
//
// Recording walks the error's own chain outside the domain lock, so a span can
// open or end while a report is in flight. Without a claim on the span, a
// report that began before a span could become that span's cause, and a report
// that began inside one could miss its End -- the operation label and the cause
// would end up on different spans.
func TestAReportBelongsToTheSpanItStartedIn(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	held := newBlocking("a release that began before any span")

	reported := make(chan struct{})
	go func() {
		defer close(reported)
		site.Cleanup(held)
	}()
	<-held.entered

	// A span opens while that report is still inside the ledger's error walk.
	// It has nothing to do with it, and must end with nothing.
	unrelated := domain.Open(Flush)
	if !unrelated.Clean() {
		t.Fatal("a report claimed before Open dirtied the new span")
	}
	close(held.release)
	<-reported
	if cause := unrelated.End(); cause != nil {
		t.Fatalf("an unrelated span ended with %v, which began before it opened", cause)
	}
	if got := ledger.Occurrences(); got != 1 {
		t.Fatalf("occurrences = %d, want the one release", got)
	}
	// It belonged to no span, so it is labelled with the step the run is in.
	if events := ledger.Events(); len(events) != 1 || events[0].Operation != Run {
		t.Fatalf("events = %#v, want the release under the run's own stage", events)
	}
}

// The other direction: a report that began inside a span is part of what that
// span ends with, even if it is still being recorded when End is called.
func TestASpanWaitsForTheReportsThatBeganInsideIt(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	held := newBlocking("a release that began inside the span")

	span := domain.Open(Flush)
	reported := make(chan struct{})
	go func() {
		defer close(reported)
		site.Cleanup(held)
	}()
	<-held.entered
	if span.Clean() {
		t.Fatal("a report claimed inside the span left it clean before it committed")
	}

	ended := make(chan error, 1)
	go func() { ended <- span.End() }()
	// End cannot finish while the report it owns is still in flight.
	select {
	case cause := <-ended:
		t.Fatalf("the span ended with %v before the report inside it committed", cause)
	default:
	}
	close(held.release)
	<-reported

	cause := <-ended
	if cause == nil {
		t.Fatal("the span ended with no cause although a release inside it failed")
	}
	var reference *Cause
	if !errors.As(cause, &reference) {
		t.Fatalf("cause = %v, want a reference to the report that began inside the span", cause)
	}
	event, ok := ledger.Event(reference.Event)
	if !ok {
		t.Fatalf("cause names %+v, which the ledger does not hold", reference.Event)
	}
	if event.Operation != Flush {
		t.Fatalf("operation = %v, want the span the report began in", event.Operation)
	}
}

// A plugin's malformed error must not leave the domain's own ticket state
// broken. Inspection degrades to an opaque occurrence, and End remains able to
// close the span.
func TestPanickingUnwrapCannotStrandASpanTicket(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("keeper", "node")
	span := domain.Open(Flush)
	domain.At("node").Cleanup(panickingUnwrap{err: errors.New("bad plugin error")})

	ended := make(chan error, 1)
	go func() { ended <- span.End() }()
	select {
	case cause := <-ended:
		if cause == nil {
			t.Fatal("opaque cleanup occurrence did not become the span cause")
		}
	case <-time.After(time.Second):
		t.Fatal("Span.End waited on a ticket left by a panicking Unwrap")
	}
}

// An error graph that is nothing but a re-propagation of one cause is an echo.
// An error graph with several branches is not, however many of its branches are
// re-propagations: a cause travelling in one branch must not decide the fate of
// an independent failure travelling in another.
func TestAJoinedErrorCarryingACauseStillRecordsItsOtherBranches(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("keeper", "node")
	stopped := errors.New("what stopped the run")
	cause := domain.Perform(Run, func(*Span) error { return stopped })
	if cause == nil {
		t.Fatal("the run produced no cause")
	}
	before := ledger.Occurrences()

	independent := errors.New("a failure of its own")
	recorded := ledger.Record(Entry{Kind: WorkError, Operation: Flush, Err: errors.Join(cause, independent)})
	if recorded == nil {
		t.Fatal("a joined error recorded nothing")
	}
	if ledger.Occurrences() != before+1 {
		t.Fatalf("occurrences = %d, want exactly one new event for the independent branch", ledger.Occurrences())
	}
	found := false
	for _, event := range ledger.Events() {
		if errors.Is(event.Err, independent) {
			found = true
			if errors.Is(event.Err, stopped) {
				t.Fatal("the independent failure was recorded joined to the cause it travelled with")
			}
		}
	}
	if !found {
		t.Fatalf("the independent branch was suppressed as an echo: %#v", ledger.Events())
	}

	// A pure re-propagation, wrapper and all, is still an echo.
	for _, echo := range []error{cause, fmt.Errorf("observed at a boundary: %w", cause)} {
		count := ledger.Occurrences()
		if again := ledger.Record(Entry{Kind: WorkError, Operation: Flush, Err: echo}); again == nil {
			t.Fatal("an echo resolved to nothing")
		}
		if ledger.Occurrences() != count {
			t.Fatalf("a pure re-propagation was recorded as a new failure: %v", echo)
		}
	}
}

func TestAWrapperAroundAJoinStillSeparatesOccurrences(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("keeper", "node")
	cause := domain.Perform(Run, func(*Span) error { return errors.New("what stopped") })
	independent := errors.New("independent")
	before := ledger.Occurrences()

	ledger.Record(Entry{Kind: WorkError, Operation: Flush, Err: fmt.Errorf("at boundary: %w", errors.Join(cause, independent))})
	if got := ledger.Occurrences(); got != before+1 {
		t.Fatalf("occurrences = %d, want one independent occurrence", got)
	}
	for _, event := range ledger.Events() {
		if errors.Is(event.Err, independent) && errors.Is(event.Err, cause) {
			t.Fatalf("independent event retained the joined cause: %v", event.Err)
		}
	}
}

// A cause handed out must stay resolvable however the budget treated the event
// list. An unresolvable cause is worse than none: the boundary receiving it
// cannot recognise the echo and records the same failure again as something new.
func TestACauseStaysResolvableWhenTheEventBudgetIsExhausted(t *testing.T) {
	ledger := NewBoundedLedger(Budget{Events: 1, GroupSamples: 1, Groups: 8, Stacks: 8, StackBytes: 1 << 20})
	first := ledger.Domain("first", "a")
	second := ledger.Domain("second", "b")

	// The first domain uses up the whole cleanup budget.
	firstCause := first.Perform(Run, func(*Span) error {
		first.At("a").Cleanup(errors.New("the only release the budget keeps"))
		return nil
	})
	// The second still produces a cause, from an occurrence the budget could not
	// keep in full.
	secondCause := second.Perform(Run, func(*Span) error {
		second.At("b").Cleanup(errors.New("a release beyond the budget"))
		return nil
	})
	for name, cause := range map[string]error{"first": firstCause, "second": secondCause} {
		if cause == nil {
			t.Fatalf("%s domain produced no cause", name)
		}
		var reference *Cause
		if !errors.As(cause, &reference) {
			t.Fatalf("%s cause = %v, want a reference", name, cause)
		}
		if reference.Err == nil {
			t.Fatalf("%s cause lost its error when the representative was omitted", name)
		}
		// And re-observing it resolves rather than recording a new failure.
		before := ledger.Occurrences()
		if again := ledger.Record(Entry{Kind: WorkError, Operation: Flush, Err: cause}); again == nil || again.ID != reference.Event {
			t.Fatalf("%s echo = %#v, want the event it names", name, again)
		}
		if ledger.Occurrences() != before {
			t.Fatalf("%s echo was recorded as a new failure", name)
		}
	}
}

// The cleanup cap is a cap. Nothing gets past it, including the first
// occurrence of a class -- a class that keeps no sample still keeps its count,
// its identities, and the fact that its detail was dropped.
func TestTheCleanupCapHoldsAcrossClasses(t *testing.T) {
	ledger := NewBoundedLedger(Budget{Events: 1, GroupSamples: 0, Groups: 8, Stacks: 8, StackBytes: 1 << 20})
	domain := ledger.Domain("keeper", "node")
	for index := range 3 {
		domain.At("node").Cleanup(distinctClass(index))
	}
	if events := ledger.Events(); len(events) != 0 {
		t.Fatalf("retained %d events, want no samples at GroupSamples 0: %#v", len(events), events)
	}
	if got := ledger.Occurrences(); got != 3 {
		t.Fatalf("occurrences = %d, want every one counted", got)
	}
	var total uint64
	for _, group := range ledger.Groups() {
		total += group.Count
	}
	if total != 3 {
		t.Fatalf("counted %d across classes, want 3", total)
	}
}

func TestWorkJoinStormCannotBypassTheEventBudget(t *testing.T) {
	ledger := NewBoundedLedger(Budget{Events: 2, GroupSamples: 1, Groups: 4})
	parts := make([]error, 100)
	for index := range parts {
		parts[index] = errors.New("the same work failure class")
	}
	ledger.Record(Entry{Kind: WorkError, Operation: Run, Err: errors.Join(parts...)})
	if got := ledger.Occurrences(); got != uint64(len(parts)) {
		t.Fatalf("occurrences = %d, want %d decomposed work failures", got, len(parts))
	}
	if events := ledger.Events(); len(events) > 2 {
		t.Fatalf("retained %d work samples, want the global cap", len(events))
	}
}

func TestSelfContainedCauseStaysAnEchoWhenNoSamplesFit(t *testing.T) {
	ledger := NewBoundedLedger(Budget{})
	domain := ledger.Domain("keeper", "node")
	cause := domain.Perform(Run, func(*Span) error { return errors.New("stopped work") })
	if cause == nil {
		t.Fatal("span returned no cause")
	}
	if len(ledger.Events()) != 0 {
		t.Fatal("zero sample budget retained an event")
	}
	before := ledger.Occurrences()
	again := ledger.Record(Entry{Kind: WorkError, Operation: Flush, Err: cause})
	if again == nil || again.ID.Seq == 0 || ledger.Occurrences() != before {
		t.Fatalf("self-contained cause was not resolved as an echo: %#v", again)
	}
}

func TestOmittedCleanupCauseEchoKeepsItsCleanupClassification(t *testing.T) {
	ledger := NewBoundedLedger(Budget{})
	domain := ledger.Domain("keeper", "node")
	cause := domain.Perform(Run, func(*Span) error {
		domain.At("node").Cleanup(errors.New("release failed"))
		return nil
	})
	if cause == nil {
		t.Fatal("cleanup-only span returned no cause")
	}
	domain.Perform(Flush, func(*Span) error { return cause })
	stopped, ok := ledger.Stopping()
	if !ok || !stopped.Kind.Cleanup() {
		t.Fatalf("stopping = %#v, cleanup echo became a work provenance", stopped)
	}
}

func TestCauseEchoRestoresOmittedAttributionAfterStoppingWorkReplacesIt(t *testing.T) {
	for _, test := range []struct {
		name      string
		budget    Budget
		wantStack bool
	}{
		{name: "retained", budget: Budget{Events: 8, GroupSamples: 8, Groups: 8, Stacks: 8, StackBytes: 1 << 20}, wantStack: true},
		{name: "omitted", budget: Budget{Events: 0, GroupSamples: 0, Groups: 8, Stacks: 8, StackBytes: 1 << 20}, wantStack: true},
		{name: "zero", budget: Budget{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ledger := NewBoundedLedger(test.budget)
			domain := ledger.Domain("cleanup-task", "home")
			cleanupStack := []byte("cleanup-stack")
			cleanupError := &PanicError{Name: "cleanup-task", Location: "declared-node", Summary: "safe", Stack: cleanupStack}
			cleanupCause := domain.Perform(Close, func(*Span) error {
				domain.At("declared-node").Cleanup(cleanupError)
				return nil
			})
			if cleanupCause == nil {
				t.Fatal("cleanup span returned no cause")
			}
			var cleanupReference *Cause
			if !errors.As(cleanupCause, &cleanupReference) {
				t.Fatal("cleanup span returned a non-cause error")
			}
			// A later work failure replaces the ledger's stopping snapshot. The
			// cleanup Cause must remain sufficient to reconstruct its own event.
			domain.Perform(Run, func(*Span) error { return errors.New("work failed") })
			before := ledger.Occurrences()

			echo := domain.Perform(Flush, func(*Span) error { return cleanupCause })
			if got := ledger.Occurrences(); got != before {
				t.Fatalf("echo occurrences = %d, want unchanged at %d", got, before)
			}
			var reference *Cause
			if !errors.As(echo, &reference) {
				t.Fatalf("echo = %v, want a cause", echo)
			}
			if got := OperationOf(reference); got != Close {
				t.Errorf("echo operation = %v, want %v", got, Close)
			}
			if reference.kind != CleanupPanic || reference.task != "cleanup-task" || reference.node != "declared-node" {
				t.Errorf("echo attribution = kind %v task %q node %q, want cleanup panic/cleanup-task/declared-node", reference.kind, reference.task, reference.node)
			}
			if test.wantStack && string(reference.stack) != string(cleanupStack) {
				t.Errorf("echo stack = %q, want %q", reference.stack, cleanupStack)
			}
			if !test.wantStack && len(reference.stack) != 0 {
				t.Errorf("echo stack = %q, want it omitted by the zero stack budget", reference.stack)
			}
			if len(reference.stack) != 0 && &reference.stack[0] != &cleanupReference.stack[0] {
				t.Error("echo Cause copied the interned stack instead of sharing the immutable depot slice")
			}
			if !errors.Is(reference, cleanupReference.Err) {
				t.Error("echo cause did not preserve the original cleanup error")
			}
		})
	}
}

// The overflow group counts distinct classes, not occurrences, and says so when
// it can no longer count them exactly.
func TestOverflowCountsDistinctClassesAndAdmitsWhenItCannot(t *testing.T) {
	ledger := NewBoundedLedger(Budget{Events: 4, GroupSamples: 1, Groups: 1, Stacks: 8, StackBytes: 1 << 20})
	domain := ledger.Domain("keeper", "node")
	site := domain.At("node")
	// One class, recorded first, occupies the single tracked slot.
	site.Cleanup(distinctClass(0))
	// A second class overflows, and repeating it must not count as more classes.
	for range 5 {
		site.Cleanup(distinctClass(1))
	}
	overflow := overflowGroup(t, ledger)
	if overflow.Classes != 1 {
		t.Fatalf("overflow folded %d classes, want 1: repetition of one class is not several classes", overflow.Classes)
	}
	if overflow.Count != 5 {
		t.Fatalf("overflow count = %d, want every occurrence", overflow.Count)
	}
	if overflow.ClassesTruncated {
		t.Fatal("the class count was reported as a lower bound although it is exact")
	}

	// Past the bounded set the count becomes a lower bound, and says so rather
	// than presenting itself as exact.
	for index := 2; index < 40; index++ {
		site.Cleanup(distinctClass(index))
	}
	overflow = overflowGroup(t, ledger)
	if !overflow.ClassesTruncated {
		t.Fatal("more classes were folded in than could be counted, and the count did not admit it")
	}
	if overflow.Classes > overflow.Count {
		t.Fatalf("classes = %d exceeds occurrences = %d", overflow.Classes, overflow.Count)
	}
}

func TestOverflowIsOneGlobalGroupAcrossCoarseMetadata(t *testing.T) {
	ledger := NewBoundedLedger(Budget{Events: 8, GroupSamples: 1, Groups: 1})
	for _, entry := range []Entry{
		{Kind: CleanupError, Operation: Run, Task: "a", Node: "one", Err: distinctClass(0)},
		{Kind: WorkError, Operation: Flush, Task: "b", Node: "two", Err: distinctClass(1)},
		{Kind: CleanupPanic, Operation: Close, Task: "c", Node: "three", Err: distinctClass(2)},
	} {
		ledger.Record(entry)
	}
	groups := ledger.Groups()
	if len(groups) != 2 {
		t.Fatalf("groups = %#v, want one tracked class plus one global overflow", groups)
	}
	if !overflowGroup(t, ledger).ClassesTruncated {
		t.Fatal("overflow did not mark its class count as a lower bound")
	}
}

func overflowGroup(t testing.TB, ledger *Ledger) Group {
	t.Helper()
	for _, group := range ledger.Groups() {
		if group.Overflow() {
			return group
		}
	}
	t.Fatalf("no overflow group in %#v", ledger.Groups())
	return Group{}
}
