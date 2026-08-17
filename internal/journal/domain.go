package journal

import (
	"runtime/debug"
	"sync"

	"github.com/godexture/godec/internal/ownership"
)

// Domain is a stable failure domain: the place an ownership slot reports a
// release it could not perform.
//
// Its lifetime is the whole run, which is the only lifetime that can be at
// least as long as every slot bound to it. A slot outlives the call that
// filled it whenever the contract allows -- a collector or transport keeping a
// cell across calls, a component releasing what it retained during Flush or
// Close, a queue holding payloads until the run discards them -- so a domain
// cannot be a lifecycle operation's object. Operations are Spans, and a domain
// carries many of them in sequence.
//
// A domain is written by more than one goroutine over its life: its task
// records failures while it runs, and the run's own goroutine performs the
// Flush, Close and Discard steps afterwards. Everything shared is behind mu or
// an atomic. The home node is immutable and identifies work that is not tied to
// an ownership Site.
type Domain struct {
	ledger *Ledger
	name   string
	home   string

	mu   sync.Mutex
	span *Span
	// settled wakes a span that is ending and still has reports in flight.
	settled *sync.Cond
}

func (d *Domain) Name() string {
	if d == nil {
		return ""
	}
	return d.name
}

// Home returns the node this domain belongs to.
func (d *Domain) Home() string {
	if d == nil {
		return ""
	}
	return d.home
}

// Ledger returns the run ledger this domain writes to.
func (d *Domain) Ledger() *Ledger {
	if d == nil {
		return nil
	}
	return d.ledger
}

// At returns this domain's reporting face at one node.
//
// A Site is what an ownership slot binds to. It is immutable and outlives
// every span, so a slot bound during Run still reports somewhere the run
// collects from during Flush, Close, or after the task has joined. The node is
// fixed at binding rather than read from wherever the domain happens to be:
// the stage that declared the slot is the stage whose declared Drop failed.
func (d *Domain) At(node string) *Site {
	if d == nil {
		return nil
	}
	site := &Site{domain: d, node: node}
	if d.ledger != nil && d.ledger.ownership != nil {
		site.reporter = ownership.Wrap(site, func(delta int64) {
			d.ledger.ownership.add(node, delta)
		})
	}
	return site
}

// Site is one node's reporting face onto a domain. It satisfies flow.Reporter
// and flow.Owner.
type Site struct {
	domain   *Domain
	node     string
	reporter Reporter
}

// Reporter is the release-reporting face Item binds to. It is Site itself in
// an ordinary run and an audited wrapper only when the Ledger opted in before
// creating the Domain.
type Reporter interface {
	Cleanup(error)
}

func (s *Site) Reporter() Reporter {
	if s == nil {
		// Preserve the typed nil reporting face used by standalone runtime
		// binding tests: it declares a slot while Cleanup remains a no-op.
		return s
	}
	if s.reporter != nil {
		return s.reporter
	}
	return s
}

// Cleanup records a release this site's owner could not perform. It is the
// flow.Reporter end of the journal: releasing happens where no return value is
// left, so a slot reports instead of returning.
func (s *Site) Cleanup(err error) {
	if s == nil {
		return
	}
	s.domain.report(s.node, err)
}

// Domain returns the domain this site reports to.
func (s *Site) Domain() *Domain {
	if s == nil {
		return nil
	}
	return s.domain
}

// ticket is a claim on the span a report belongs to.
//
// Recording walks the error's own chain, which is code the domain does not
// own, so it must not run under the domain's lock. That leaves a window, and
// without a ticket the window is a hole: a report could be labelled with one
// span's operation and then observed by whichever span happened to be open
// when it finished, so a report that began before a span could become that
// span's cause, and a report that began inside a span could miss its End. A
// ticket fixes the span at the moment the report starts and holds that span
// open until the report commits to it.
type ticket struct {
	span      *Span
	operation Operation
}

// claim fixes which span a report belongs to and registers it as in flight.
func (d *Domain) claim() ticket {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.span == nil {
		return ticket{}
	}
	d.span.pending++
	d.span.dirty.Store(true)
	return ticket{span: d.span, operation: d.span.operation}
}

// commit hands the recorded failure to the span the report was claimed for,
// never to whichever span is open now, and releases the claim.
func (d *Domain) commit(claimed ticket, failure *Failure) {
	if claimed.span == nil {
		return
	}
	d.mu.Lock()
	if failure != nil {
		claimed.span.stop(*failure)
	}
	claimed.span.pending--
	if claimed.span.pending == 0 {
		d.settled.Broadcast()
	}
	d.mu.Unlock()
}

// report writes a release failure to the ledger under the operation the domain
// is currently performing.
//
// A domain with an open span is inside a lifecycle operation and the failure
// belongs to it. A domain with no span open still exists -- a component's
// owner does, for the whole run -- and its releases are labelled with the step
// the run itself is in, so a retained payload released between two steps is
// still placed rather than left unlabelled. Neither case can lose the failure:
// the ledger is not a boundary and nothing about it ends.
func (d *Domain) report(node string, err error) {
	if d == nil || err == nil {
		return
	}
	claimed := d.claim()
	var failure *Failure
	defer func() { d.commit(claimed, failure) }()
	failure = d.ledger.Record(Entry{
		Kind:      CleanupError,
		Operation: claimed.operation,
		Task:      d.name,
		Node:      node,
		Err:       err,
	})
}

// Fail records a failure of work at this site and returns a reference to it.
//
// A stage that closes several things reports each one as it happens rather
// than gathering them into a single error: two components failing to flush are
// two failures, and joining them before the ledger sees them would make them
// one event that no consumer can take apart again. What comes back is a
// reference the caller propagates for control flow, which the ledger resolves
// rather than recording a second time.
func (s *Site) Fail(err error) error {
	if s == nil || err == nil {
		return err
	}
	return s.record(WorkError, err, nil)
}

// Perform invokes one third-party lifecycle callback at this site. It records
// an error or panic as this site's independent work occurrence and lets the
// caller continue closing downstream components or sibling branches.
//
// It deliberately does not open a Span: the caller already owns the operation
// span, and this site only supplies the node-local failure boundary inside it.
func (s *Site) Perform(work func() error) (cause error) {
	if s == nil {
		return work()
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			stack := debug.Stack()
			cause = s.record(WorkPanic, &PanicError{
				Name:     s.domain.name,
				Location: s.node,
				Summary:  recoveredSummary(recovered),
				Stack:    stack,
			}, stack)
		}
	}()
	return s.Fail(work())
}

func (s *Site) record(kind Kind, err error, stack []byte) error {
	if s == nil || err == nil {
		return err
	}
	claimed := s.domain.claim()
	var failure *Failure
	defer func() { s.domain.commit(claimed, failure) }()
	failure = s.domain.ledger.Record(Entry{
		Kind:      kind,
		Operation: claimed.operation,
		Task:      s.domain.name,
		Node:      s.node,
		Err:       err,
		Stack:     stack,
	})
	if failure == nil {
		return err
	}
	return newCause(*failure)
}
