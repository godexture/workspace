// Package task owns cancellable, joinable job tasks. It is the only runtime
// boundary that recovers task panics.
package task

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/godexture/godec/internal/journal"
)

var (
	ErrClosed      = errors.New("task group no longer accepts work")
	ErrInvalidTask = errors.New("task name and function are required")
)

// nextTaskID identifies journals across every group in the process. A name is
// chosen for people and nothing keeps two tasks from sharing one.
var nextTaskID atomic.Uint64

// Report is a stable snapshot. Running is populated when the wait context
// expires; the group never claims those tasks have stopped.
type Report struct {
	Outcomes []journal.Outcome
	Running  []string
	WaitErr  error
}

func (r Report) Complete() bool { return r.WaitErr == nil && len(r.Running) == 0 }

// Group owns one cancellation tree and every task registered with it.
type Group struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	fail   func(error)

	mu        sync.Mutex
	accepting bool
	next      uint64
	active    int
	running   map[uint64]string
	outcomes  []journal.Outcome
	done      chan struct{}
	doneOnce  sync.Once
}

func New(parent context.Context) *Group {
	return NewLinked(parent, nil)
}

// NewLinked reports the first-class failure of any task to the owner of a
// larger cancellation tree. The callback must not wait on this Group.
func NewLinked(parent context.Context, fail func(error)) *Group {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &Group{
		ctx:       ctx,
		cancel:    cancel,
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

// Start registers work before the group is sealed. It satisfies
// plugin.TaskStarter without exposing cancellation or join ownership to a
// component.
func (g *Group) Start(name string, work func(context.Context) error) error {
	return g.StartScoped(name, journal.New(journal.Run, ""), work)
}

// StartScoped is the runtime-only form used by an execution island, which needs
// the scope itself so it can hand the same failure domain to every ownership
// slot the task owns.
func (g *Group) StartScoped(name string, scope *journal.Scope, work func(context.Context) error) error {
	if g == nil || strings.TrimSpace(name) == "" || work == nil {
		return ErrInvalidTask
	}
	if scope == nil {
		scope = journal.New(journal.Run, "")
	}
	g.mu.Lock()
	if !g.accepting || g.ctx.Err() != nil {
		g.mu.Unlock()
		return ErrClosed
	}
	id := g.next
	g.next++
	g.active++
	g.running[id] = name
	g.mu.Unlock()
	// The journal's identity is process-wide, not group-local: a job runs more
	// than one group, and a consumer that collects from all of them must not
	// have two tasks claim the same events.
	scope.Attach(nextTaskID.Add(1), name)

	go g.run(id, name, scope, work)
	return nil
}

// run assembles the task's result in one place through the same boundary a
// Host-driven Finish hand-off uses, so a panic here and a panic there lose
// nothing differently.
func (g *Group) run(id uint64, name string, scope *journal.Scope, work func(context.Context) error) {
	g.finish(id, journal.Capture(scope, func() error { return work(g.ctx) }))
}

func (g *Group) finish(id uint64, outcome journal.Outcome) {
	// A task cancelled by a peer's failure is not a second failure. Only the
	// primary can be that echo; a release that failed during the cancellation
	// still happened and is still reported.
	if outcome.Primary != nil && cancellationEcho(*outcome.Primary, g.ctx) {
		outcome.Primary = nil
	}
	cause := outcome.Cause()
	g.mu.Lock()
	delete(g.running, id)
	g.active--
	if outcome.Failed() {
		g.outcomes = append(g.outcomes, outcome)
	}
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

func cancellationEcho(err error, ctx context.Context) bool {
	if ctx.Err() == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Cause(ctx))
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
	result := Report{
		Outcomes: append([]journal.Outcome(nil), g.outcomes...),
		Running:  make([]string, 0, len(g.running)),
		WaitErr:  waitErr,
	}
	for _, name := range g.running {
		result.Running = append(result.Running, name)
	}
	sort.Strings(result.Running)
	return result
}

func (g *Group) closeDone() { g.doneOnce.Do(func() { close(g.done) }) }
