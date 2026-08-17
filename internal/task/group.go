// Package task owns cancellable, joinable job tasks. It is the only runtime
// boundary that starts one, and every task it starts performs its work inside
// a lifecycle span on its own failure domain.
package task

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/godexture/godec/internal/cancel"
	"github.com/godexture/godec/internal/errorx"
	"github.com/godexture/godec/internal/journal"
)

var (
	ErrClosed      = errors.New("task group no longer accepts work")
	ErrInvalidTask = errors.New("task name and function are required")
	ErrDomain      = errors.New("task requires a failure domain")
)

// Report is a stable snapshot of what joining found. It carries no failures:
// those are in the run ledger, recorded where they happened, and a second
// carrier for them would be a second thing to keep in sync. What only joining
// can know is which tasks did not stop and why the wait ended.
type Report struct {
	Running []string
	WaitErr error
}

func (r Report) Complete() bool { return r.WaitErr == nil && len(r.Running) == 0 }

// Group owns one cancellation tree and every task registered with it.
type Group struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	ledger *journal.Ledger
	fail   func(error)

	mu        sync.Mutex
	accepting bool
	next      uint64
	active    int
	running   map[uint64]string
	done      chan struct{}
	doneOnce  sync.Once
}

func New(parent context.Context, ledger *journal.Ledger) *Group {
	return NewLinked(parent, ledger, nil)
}

// NewLinked reports the cancellation cause of any failing task to the owner of
// a larger cancellation tree. The callback must not wait on this Group.
func NewLinked(parent context.Context, ledger *journal.Ledger, fail func(error)) *Group {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &Group{
		ctx:       ctx,
		cancel:    cancel,
		ledger:    ledger,
		fail:      fail,
		accepting: true,
		running:   make(map[uint64]string),
		done:      make(chan struct{}),
	}
}

func (g *Group) Context() context.Context {
	if g == nil || g.ctx == nil {
		return context.Background()
	}
	return g.ctx
}

// Start registers component-owned work. It satisfies plugin.TaskStarter
// without exposing cancellation, join ownership, or the ledger to a component,
// and gives the task a failure domain of its own so anything it owns has
// somewhere to report a release it could not perform.
func (g *Group) Start(name string, work func(context.Context) error) error {
	if g == nil || strings.TrimSpace(name) == "" || work == nil {
		return ErrInvalidTask
	}
	return g.StartDomain(g.ledger.Domain(name, ""), func(ctx context.Context, _ *journal.Span) error {
		return work(ctx)
	}, nil)
}

// StartDomain registers work on a domain the caller already owns, which is how
// an execution island hands the same failure domain to every ownership slot
// its task owns before the task exists.
//
// sealed, when non-nil, runs in this task's own goroutine after its Run span
// has ended, and before the task is reported as finished. A task finishing its
// work is not the same moment as its span ending -- the span records what work
// returned, then ends -- so a signal sent from inside work races anything the
// waiter does with the same domain. sealed is the only signal that cannot,
// which is what lets the run open a Flush span on that domain afterwards.
func (g *Group) StartDomain(domain *journal.Domain, work func(context.Context, *journal.Span) error, sealed func(error)) error {
	if g == nil || work == nil {
		return ErrInvalidTask
	}
	if domain == nil || domain.Name() == "" {
		return ErrDomain
	}
	g.mu.Lock()
	if !g.accepting || g.ctx.Err() != nil {
		g.mu.Unlock()
		// Refusing because the run is already stopping is that failure
		// reaching here, not a new one. Naming it keeps the caller's report of
		// this refusal from reading as a second, independent thing that went
		// wrong.
		if cause := g.ledger.Stopped(); cause != nil && g.ctx.Err() != nil {
			return cause
		}
		return ErrClosed
	}
	id := g.next
	g.next++
	g.active++
	g.running[id] = domain.Name()
	g.mu.Unlock()

	go g.run(id, domain, work, sealed)
	return nil
}

// run performs the task's Run operation through the same boundary every other
// lifecycle operation uses, so a panic here and a panic in a run-driven Flush
// lose nothing differently.
func (g *Group) run(id uint64, domain *journal.Domain, work func(context.Context, *journal.Span) error, sealed func(error)) {
	cause := domain.Perform(journal.Run, func(span *journal.Span) error {
		return g.settle(work(g.ctx, span))
	})
	if sealed != nil {
		sealed(cause)
	}
	g.finish(id, cause)
}

// settle keeps a task from claiming the run's own cancellation as a failure of
// its own.
//
// A task that returns an error naming a ledger event -- which is what
// context.Cause hands back, and what every peer in this codebase returns
// verbatim -- is already talking about that event, so it passes through and
// the ledger recognizes it. A task that returns bare cancellation while this
// group is cancelled has only noticed that something stopped, so it says so by
// pointing at whatever the ledger holds as the reason, and says nothing at all
// when nothing does: why a run was cancelled from outside is for the boundary
// that cancelled it to record, not for every task that noticed.
//
// Nothing here compares what two errors say. A task that genuinely fails with
// its own deadline while the group is still live keeps that failure, and two
// tasks that fail with one shared sentinel stay two failures.
func (g *Group) settle(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errorx.Find[*journal.Cause](err); ok {
		return err
	}
	if cancel.Normalize(g.ctx, err) != nil {
		return g.ledger.Stopped()
	}
	return err
}

// finish removes the task and propagates its cause.
//
// There is no echo test here any more. A task cancelled by a peer returns that
// peer's cause, and the ledger recognizes it as the event it already holds
// rather than a second failure, so nothing has to be discarded to avoid
// double-counting -- and nothing genuinely independent can be discarded by
// resembling what stopped the run.
func (g *Group) finish(id uint64, cause error) {
	g.mu.Lock()
	delete(g.running, id)
	g.active--
	done := !g.accepting && g.active == 0
	g.mu.Unlock()
	if cause != nil {
		g.cancel(cause)
		if g.fail != nil {
			g.fail(cause)
		}
	}
	if done {
		g.closeDone()
	}
}

// Cancel broadcasts a cause to every task. A nil cause means ordinary
// cancellation.
func (g *Group) Cancel(cause error) {
	if g == nil || g.cancel == nil {
		return
	}
	if cause == nil {
		cause = context.Canceled
	}
	g.cancel(cause)
}

// Seal prevents new work. It is idempotent.
func (g *Group) Seal() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.accepting = false
	done := g.active == 0
	g.mu.Unlock()
	if done {
		g.closeDone()
	}
}

// Wait seals the group and waits only as long as wait permits. A timed-out
// report names every still-running task.
func (g *Group) Wait(wait context.Context) Report {
	if g == nil {
		return Report{}
	}
	if wait == nil {
		wait = context.Background()
	}
	g.Seal()
	select {
	case <-g.done:
		return g.report(nil)
	case <-wait.Done():
		return g.report(wait.Err())
	}
}

func (g *Group) report(waitErr error) Report {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := Report{Running: make([]string, 0, len(g.running)), WaitErr: waitErr}
	for _, name := range g.running {
		result.Running = append(result.Running, name)
	}
	sort.Strings(result.Running)
	return result
}

func (g *Group) closeDone() { g.doneOnce.Do(func() { close(g.done) }) }
