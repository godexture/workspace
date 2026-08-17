package journal

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/godexture/godec/internal/errorx"
)

// nextRun numbers ledgers so an EventID that escapes one run cannot be read as
// an event of another. It is the only process-wide counter this package has:
// everything inside a run is numbered by that run's own ledger.
var nextRun atomic.Uint64

// Ledger is the failure evidence of one run.
//
// It outlives every domain, span, task, queue, operator and resource the run
// creates, and it is append-only: a failure recorded here is never cleared,
// replaced, or made unreachable by a boundary ending. Nothing else is a
// failure's only home -- an outcome a panic discards, a return value nobody
// reads, a journal a second reader never seals -- because everything else is
// shorter-lived than the evidence it would have to carry.
//
// It is bounded and loss-aware rather than unbounded. Every occurrence is
// numbered and counted; how many are kept in full is a Budget. What is never
// dropped is the fact that a failure happened, how many times, what class it
// was, which event was first and last, and that detail was omitted. See
// Budget.
//
// Recording is the only mutation, and it happens on failure paths, so the mutex
// here is never on an item loop.
type Ledger struct {
	run    uint64
	budget Budget
	// ownership is optional and installed before a run publishes any Domain.
	// It stays nil for ordinary runs, keeping item accounting free of atomics
	// and allocations unless the caller explicitly asks for verification.
	ownership *ownershipTracker

	mu     sync.Mutex
	depot  *depot
	events []Failure
	// seq numbers every occurrence, retained in full or not, so identity never
	// depends on what the budget kept.
	seq     uint64
	groups  map[Class]*Group
	order   []*Group
	tracked int
	// folded remembers which classes went into the overflow group, so its
	// distinct-class count is exact while the set fits.
	folded map[Class]struct{}
	// stopped is the one provenance record retained independently of event
	// samples, so the run can still explain why it stopped at any budget.
	stopped Failure
	// stage labels failures reported outside any span -- a component's
	// persistent slot released between lifecycle steps -- with the step the run
	// as a whole is in.
	stage Operation
}

// NewLedger opens the ledger for one run under the default evidence budget.
func NewLedger() *Ledger { return NewBoundedLedger(DefaultBudget()) }

// NewBoundedLedger opens a ledger with an explicit budget. Production uses
// DefaultBudget; a test uses this to reach the truncation paths with a budget
// small enough to exercise them.
func NewBoundedLedger(budget Budget) *Ledger {
	budget = budget.normalize()
	return &Ledger{
		run:    nextRun.Add(1),
		budget: budget,
		depot:  newDepot(budget),
		groups: make(map[Class]*Group),
		folded: make(map[Class]struct{}),
		stage:  Run,
	}
}

// Entry is one failure being offered to the ledger.
type Entry struct {
	Kind      Kind
	Operation Operation
	Task      string
	Node      string
	Err       error
	Stack     []byte
}

// Record accounts for one failure and returns it.
//
// It returns an already-recorded failure instead, accounting for nothing, when
// the error is a second sighting of an event this ledger already holds. There
// is exactly one way that happens, and it is identity rather than resemblance:
// the error carries a *Cause naming the event. Everything downstream of a
// cancellation observes that exact value, because context.Cause hands it back
// verbatim and this codebase's peers return it verbatim.
//
// Two independent failures that read identically -- the same sentinel returned
// by two tasks -- are therefore two events, and one failure observed at four
// boundaries is one event, no matter how many of them look at it. Nothing is
// ever folded for saying the same thing as something else, so a genuine second
// failure cannot disappear into the first by coincidence.
//
// A failure the budget does not keep in full is still numbered, still counted
// against its class, and still returned to the caller. It is simply not added
// to the event list.
func (l *Ledger) Record(entry Entry) *Failure {
	if l == nil || entry.Err == nil {
		return nil
	}
	// Everything that walks a third-party error chain happens here, outside the
	// lock. Unwrap belongs to whoever built the error, and running plugin code
	// while holding a mutex other goroutines need is the shape of deadlock this
	// design avoids everywhere else too.
	//
	// One error value can carry several independent occurrences. A cause
	// travelling in one branch of a join must not decide the fate of an
	// independent failure travelling in another, so each branch is offered on
	// its own: the branches that are nothing but a re-propagation resolve, and
	// the rest become their own events.
	parts := branches(entry.Err)
	var first *Failure
	for _, part := range parts {
		if recorded := l.record(entry, part); recorded != nil && first == nil {
			first = recorded
		}
	}
	return first
}

func (l *Ledger) record(entry Entry, err error) *Failure {
	cause := causeOf(err)
	class := safeFailureClass(err)
	stack := entry.Stack
	if len(stack) == 0 {
		stack = safeStack(err)
	}
	entry.Err = err
	panicError, protected := errorx.Find[*PanicError](err)
	_, recovered := errorx.RecoveredPanic(err)
	if (protected && panicError != nil) || recovered {
		// Record receives one occurrence at a time. A protected child can carry
		// its recovered panic through an errors.Join returned by a composite;
		// normalize only that branch, never the independent error beside it.
		entry.Kind = entry.Kind.panicked()
		if panicError != nil {
			if panicError.Location != "" {
				entry.Node = panicError.Location
			}
			if panicError.Name != "" {
				entry.Task = panicError.Name
			}
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if cause != nil && cause.Event.Run == l.run && cause.Event.Seq != 0 {
		if existing := l.at(cause.Event); existing != nil {
			return copyFailure(existing)
		}
		// The representative may have been omitted by the event budget. The
		// Cause still carries the immutable safe attribution snapshot captured
		// when that occurrence was created; returning only its identity and
		// error would turn a valid echo into an operation-less failure.
		snapshot := cause.failureSnapshot()
		return &snapshot
	}
	if entry.Operation == 0 {
		entry.Operation = l.stage
	}
	interned, kept := l.depot.intern(stack)
	l.seq++
	failure := Failure{
		ID:        EventID{Run: l.run, Seq: l.seq},
		Kind:      entry.Kind,
		Operation: entry.Operation,
		Task:      entry.Task,
		Node:      entry.Node,
		Err:       entry.Err,
		Stack:     l.depot.at(interned),
	}
	group := l.account(failure, Class{
		Task:      entry.Task,
		Node:      entry.Node,
		Operation: entry.Operation,
		Kind:      entry.Kind,
		Failure:   class,
		Stack:     interned,
	}, !kept)
	l.stopping(failure)
	if retained := l.retain(failure, group); retained != nil {
		return copyFailure(retained)
	}
	// Counted but not copied. The caller still gets what it asked for; the
	// ledger keeps the count and says so through the group.
	return &failure
}

// copyFailure keeps the mutable ledger storage behind its lock. In particular,
// the stopping provenance is replaced when a later work failure supersedes a
// cleanup one; returning &l.stopped would let a caller race that replacement
// after Record released l.mu.
func copyFailure(value *Failure) *Failure {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func safeFailureClass(err error) (class string) {
	defer func() {
		if recover() != nil {
			class = typeName(err)
		}
	}()
	return failureClass(err)
}

func safeStack(err error) (stack []byte) {
	defer func() {
		if recover() != nil {
			stack = nil
		}
	}()
	return stackOf(err)
}

// account adds one occurrence to its class and returns the group it landed in.
// The caller holds the lock.
func (l *Ledger) account(failure Failure, class Class, truncated bool) *Group {
	group, known := l.groups[class]
	if !known {
		if l.tracked >= l.budget.Groups {
			// More distinct classes than this run tracks separately. Folding
			// into a coarser class bounds the class table itself, which an
			// error whose class varies per occurrence would otherwise grow in
			// place of the event list.
			return l.overflow(failure, class, truncated)
		}
		group = &Group{Class: class, First: failure.ID, Classes: 1}
		l.groups[class] = group
		l.order = append(l.order, group)
		l.tracked++
	}
	group.Count = add(group.Count, 1)
	group.Last = failure.ID
	if truncated {
		group.Truncated = true
	}
	return group
}

// overflow folds a class the run is not tracking separately into the coarse
// group. The caller holds the lock.
//
// Counting how many distinct classes ended up here cannot be both exact and
// bounded, so it is exact while a bounded set of the classes seen still fits
// and a lower bound afterwards. Which of the two it is, is published rather
// than guessed at: Classes is what was counted and ClassesTruncated says
// whether more went uncounted.
func (l *Ledger) overflow(failure Failure, class Class, truncated bool) *Group {
	key := class.coarse()
	group, known := l.groups[key]
	if !known {
		group = &Group{Class: key, First: failure.ID}
		l.groups[key] = group
		l.order = append(l.order, group)
	}
	if _, seen := l.folded[class]; !seen {
		if len(l.folded) < l.budget.Groups {
			l.folded[class] = struct{}{}
			group.Classes = add(group.Classes, 1)
		} else {
			if group.Classes == 0 {
				group.Classes = 1
			}
			group.ClassesTruncated = true
		}
	}
	group.Count = add(group.Count, 1)
	group.Last = failure.ID
	// An overflow group has, by construction, dropped the distinctions between
	// the classes inside it.
	group.Truncated = true
	if truncated {
		group.Truncated = true
	}
	return group
}

// retain decides whether this occurrence is kept in full, and keeps it if so.
// The caller holds the lock.
//
// Every representative event is subject to both caps. The run's stopping
// provenance is retained separately by stopping, so a useful failure can
// still explain the run when its representative sample was omitted.
func (l *Ledger) retain(failure Failure, group *Group) *Failure {
	if len(l.events) >= l.budget.Events || len(group.Samples) >= l.budget.GroupSamples {
		group.Omitted = add(group.Omitted, 1)
		group.Truncated = true
		return nil
	}
	return l.append(failure, group)
}

func (l *Ledger) append(failure Failure, group *Group) *Failure {
	l.events = append(l.events, failure)
	group.Samples = append(group.Samples, failure.ID)
	return &l.events[len(l.events)-1]
}

// stopping keeps the ledger's idea of why this run stopped.
//
// The first failure wins, because everything after it happened while the run
// was already stopping. A release that failed can hold the position while
// nothing else has -- an operation ending with an unreleased payload has not
// succeeded -- but it gives way to the first failure of actual work, which is
// the reason a release failure never explains why a run stopped. The caller
// holds the lock.
func (l *Ledger) stopping(failure Failure) {
	if !l.stopped.ID.Valid() || (l.stopped.Kind.Cleanup() && !failure.Kind.Cleanup()) {
		l.stopped = failure
	}
}

// at finds a retained event by identity. Events are appended in Seq order, so
// this is a search rather than an index: the budget can leave gaps.
// The caller holds the lock.
func (l *Ledger) at(id EventID) *Failure {
	if id.Run != l.run || id.Seq == 0 {
		return nil
	}
	if l.stopped.ID == id {
		return &l.stopped
	}
	position := sort.Search(len(l.events), func(index int) bool {
		return l.events[index].ID.Seq >= id.Seq
	})
	if position < len(l.events) && l.events[position].ID.Seq == id.Seq {
		return &l.events[position]
	}
	return nil
}

// Event returns a retained representative or the stopping provenance an
// identity names. A Cause does not depend on this lookup: it carries the
// original error itself when its representative was omitted.
func (l *Ledger) Event(id EventID) (Failure, bool) {
	if l == nil {
		return Failure{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if found := l.at(id); found != nil {
		return *found, true
	}
	return Failure{}, false
}

// Events returns every failure kept in full, in the order it was recorded. This
// is the run's single collection point: a consumer reads each one exactly once,
// because the ledger holds each one exactly once.
//
// It is not every occurrence. Groups reports what repetition was counted rather
// than copied, and the two together are the whole account.
func (l *Ledger) Events() []Failure {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Failure(nil), l.events...)
}

// Groups returns one entry per failure class, in the order each was first seen,
// so a report reads the same way twice.
func (l *Ledger) Groups() []Group {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]Group, 0, len(l.order))
	for _, group := range l.order {
		value := *group
		value.Samples = append([]EventID(nil), group.Samples...)
		result = append(result, value)
	}
	return result
}

// Occurrences is how many failures happened, counting the ones only summarised.
func (l *Ledger) Occurrences() uint64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq
}

// Failed reports whether anything has been recorded.
func (l *Ledger) Failed() bool { return l.Occurrences() != 0 }

// Stopped returns the cause to cancel this run with, which is a reference to
// the event that stopped it. A boundary that observes the cancellation and
// hands it onward carries this exact value, so the ledger can recognize the
// echo later.
func (l *Ledger) Stopped() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.stopped.ID.Valid() {
		return nil
	}
	return newCause(l.stopped)
}

// Stopping returns the one provenance record retained independently of the
// representative-event budget. A collector uses it when the event cap omitted
// the failure that explains why useful work stopped.
func (l *Ledger) Stopping() (Failure, bool) {
	if l == nil {
		return Failure{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.stopped.ID.Valid() {
		return Failure{}, false
	}
	return l.stopped, true
}

// stop records which event is being propagated as this run's cancellation.
// Cause carries the original error together with its identity, so the ledger
// need not retain a second full copy merely to recognize a later echo.
func (l *Ledger) stop(failure Failure) {
	l.mu.Lock()
	l.stopping(failure)
	l.mu.Unlock()
}

// EnterStage labels the lifecycle step the run as a whole is in and returns the
// label it replaced.
//
// A domain reports under its innermost open span. A domain with no span open --
// the owner a component holds across its whole lifecycle, releasing a retained
// payload somewhere between two steps -- reports under this instead, so a late
// release is still placed in the run rather than left unlabelled.
func (l *Ledger) EnterStage(operation Operation) Operation {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	previous := l.stage
	l.stage = operation
	l.mu.Unlock()
	return previous
}

// Domain opens a stable failure domain. name identifies its owner for a reader,
// node is where releases it is told about happened by default.
func (l *Ledger) Domain(name, node string) *Domain {
	if l == nil {
		return nil
	}
	domain := &Domain{ledger: l, name: name, home: node}
	domain.settled = sync.NewCond(&domain.mu)
	return domain
}
