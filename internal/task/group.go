// Package task owns cancellable, joinable job tasks. It is the only runtime
// boundary that recovers task panics.
package task

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
)

var (
	ErrClosed      = errors.New("task group no longer accepts work")
	ErrInvalidTask = errors.New("task name and function are required")
)

// PanicError preserves the recovered value and stack from one task boundary.
type PanicError struct {
	Name  string
	Value any
	Stack []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("task %q panicked: %v", e.Name, e.Value)
}

// Failure associates an error with the task that returned or panicked.
type Failure struct {
	Name string
	Err  error
}

func (f Failure) Panicked() bool {
	var value *PanicError
	return errors.As(f.Err, &value)
}

// Report is a stable snapshot. Running is populated when the wait context
// expires; the group never claims those tasks have stopped.
type Report struct {
	Failures []Failure
	Running  []string
	WaitErr  error
}

func (r Report) Complete() bool { return r.WaitErr == nil && len(r.Running) == 0 }

// Group owns one cancellation tree and every task registered with it.
type Group struct {
	ctx    context.Context
	cancel context.CancelCauseFunc

	mu        sync.Mutex
	accepting bool
	next      uint64
	active    int
	running   map[uint64]string
	failures  []Failure
	done      chan struct{}
	doneOnce  sync.Once
}

func New(parent context.Context) *Group {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancelCause(parent)
	return &Group{
		ctx:       ctx,
		cancel:    cancel,
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
	if g == nil || strings.TrimSpace(name) == "" || work == nil {
		return ErrInvalidTask
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

	go g.run(id, name, work)
	return nil
}

func (g *Group) run(id uint64, name string, work func(context.Context) error) {
	var failure error
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = &PanicError{Name: name, Value: recovered, Stack: append([]byte(nil), debug.Stack()...)}
		}
		g.finish(id, name, failure)
	}()
	failure = work(g.ctx)
}

func (g *Group) finish(id uint64, name string, failure error) {
	g.mu.Lock()
	delete(g.running, id)
	g.active--
	if failure != nil && !cancellationEcho(failure, g.ctx) {
		g.failures = append(g.failures, Failure{Name: name, Err: failure})
	}
	done := !g.accepting && g.active == 0
	g.mu.Unlock()
	if failure != nil {
		g.cancel(failure)
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
		Failures: append([]Failure(nil), g.failures...),
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
