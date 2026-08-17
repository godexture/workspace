package journal

import (
	"runtime/debug"
	"sync/atomic"
)

// Span is one lifecycle operation: its extent, its attribution, and its
// recovery boundary.
//
// Exactly one goroutine performs an operation, so a span has one writer. Spans
// nest rather than replace one another: a bounded edge's drain task performs a
// genuine Flush -- its downstream close -- inside the Run it is still
// executing, and opening a Flush span there labels that work correctly without
// relabeling anything or letting a second goroutine near it. Whoever opens a
// span ends it.
//
// A span holds no failures. It records them in the ledger as they happen and
// keeps only which event stopped it, so ending a span cannot make evidence
// unreachable and a failure arriving after it ends is still collected.
type Span struct {
	domain    *Domain
	parent    *Span
	operation Operation
	// stopping is the first event this span saw. A later work event replaces a
	// cleanup event, because cleanup alone is not a useful-work stop reason.
	// It is kept by value so the cause remains self-contained when the ledger's
	// representative-event budget omits the sample.
	stopping Failure
	// pending counts reports that claimed this span and have not committed to
	// it yet. End waits them out, so what a span ends with is everything that
	// began inside it.
	pending int
	ended   bool
	// dirty belongs to this span, not its domain. claim sets it at report start,
	// which is the same linearization point that chooses the span.
	dirty atomic.Bool
}

// Open begins a lifecycle operation on this domain.
func (d *Domain) Open(operation Operation) *Span {
	if d == nil {
		return nil
	}
	span := &Span{domain: d, operation: operation}
	d.mu.Lock()
	span.parent = d.span
	d.span = span
	d.mu.Unlock()
	return span
}

// Perform runs one lifecycle operation end to end: it opens a span, recovers a
// panic from work, records what stopped it, and ends the span exactly once
// regardless of how work returns.
//
// This is the one boundary every performer of a lifecycle operation uses,
// whether that performer is a task's own goroutine returning from its loop, a
// drain task closing what it feeds on EOF, or the run reaching across to
// finish a source or a join after it has joined. Without a shared boundary a
// panic during the least attended of those would discard what it recorded the
// way an unrecovered panic used to discard a return value.
//
// What it returns is the cause to cancel with: a reference to the ledger event
// that stopped this operation, or nil. Nothing else is returned, so there is
// no second error path a caller could read instead of the ledger.
func (d *Domain) Perform(operation Operation, work func(*Span) error) (cause error) {
	span := d.Open(operation)
	defer func() {
		if recovered := recover(); recovered != nil {
			span.Panicked(recovered, debug.Stack())
		}
		cause = span.End()
	}()
	span.Fail(work(span))
	return
}

// Operation returns the lifecycle step this span covers.
func (s *Span) Operation() Operation {
	if s == nil {
		return 0
	}
	return s.operation
}

// Fail records the error that stopped this operation. The first one wins:
// later failures are consequences of it, and an operation stops once.
//
// An error that merely re-observes a failure already in the ledger -- a peer
// returning context.Cause(ctx) verbatim -- becomes this span's cause without
// adding an event, because it is that event.
func (s *Span) Fail(err error) {
	if s == nil || err == nil {
		return
	}
	s.record(Entry{Kind: WorkError, Err: err})
}

// Panicked records a recovered panic as what stopped this operation. The value
// is described, never kept: it is chosen by the code that panicked and can be
// the data it was handling.
func (s *Span) Panicked(recovered any, stack []byte) {
	if s == nil {
		return
	}
	s.record(Entry{
		Kind: WorkPanic,
		Err: &PanicError{
			Name:     s.domain.name,
			Location: s.domain.home,
			Summary:  recoveredSummary(recovered),
			Stack:    append([]byte(nil), stack...),
		},
		Stack: stack,
	})
}

func (s *Span) record(entry Entry) {
	entry.Operation = s.operation
	entry.Task = s.domain.name
	entry.Node = s.domain.home
	s.domain.mu.Lock()
	s.pending++
	s.dirty.Store(true)
	s.domain.mu.Unlock()
	// The ledger is entered without the domain lock: recording walks the
	// error's own chain, which is code the domain does not own. The claim above
	// is what keeps this span from ending in the meantime.
	var failure *Failure
	defer func() {
		s.domain.mu.Lock()
		if failure != nil {
			s.stop(*failure)
		}
		s.pending--
		if s.pending == 0 {
			s.domain.settled.Broadcast()
		}
		s.domain.mu.Unlock()
	}()
	failure = s.domain.ledger.Record(entry)
}

// stop keeps the earliest event, except that useful work replaces a cleanup
// event that could not by itself explain why the operation stopped. The caller
// holds the domain lock.
func (s *Span) stop(failure Failure) {
	if !s.stopping.ID.Valid() || (s.stopping.Kind.Cleanup() && !failure.Kind.Cleanup()) {
		s.stopping = failure
	}
}

// Clean reports whether no report began in this span. The report ticket and
// dirty flag share one linearization point, so a report claimed before Open
// cannot dirty the new span and one claimed inside cannot be missed while its
// error is still being inspected.
func (s *Span) Clean() bool {
	return s == nil || !s.dirty.Load()
}

// End closes the operation and returns the cause to propagate.
//
// The cause is what stopped the work, or -- when nothing did -- the first
// release this operation could not perform, because an operation that ends
// still holding an unreleased payload has not succeeded. It never replaces a
// failure that did stop the work.
//
// Ending clears nothing. Everything this span recorded is in the ledger, and
// anything the domain is told afterwards goes to the same place.
func (s *Span) End() error {
	if s == nil || s.ended {
		return nil
	}
	s.ended = true
	s.domain.mu.Lock()
	// A report that claimed this span belongs to it, so ending waits for it to
	// settle. Error graph inspection is third-party code and happens lock-free;
	// a plugin that blocks forever in Unwrap blocks its own reporting goroutine
	// and this operation, just like one that blocks forever in Flush. Panics are
	// recovered by the ticket defer and cannot strand this wait.
	for s.pending > 0 {
		s.domain.settled.Wait()
	}
	if s.domain.span == s {
		s.domain.span = s.parent
	}
	failure := s.stopping
	s.domain.mu.Unlock()
	if !failure.ID.Valid() {
		return nil
	}
	s.domain.ledger.stop(failure)
	return newCause(failure)
}
