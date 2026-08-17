// Task is one runnable unit the execution island schedules: a source pump, a
// bounded edge's drain, or a fan-in join. Every task carries the failure
// domain it and everything it owns report to.
package drive

import (
	"context"
	"errors"
	"io"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/errorx"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/media/schema"
)

type Task struct {
	domain  *journal.Domain
	run     func(context.Context, *journal.Span) error
	barrier func(context.Context) error
	finish  func(context.Context) error
	abort   func()
	discard func()
	sealed  func(error)
}

// errAbandoned is runtime control, not a component failure. It travels only
// far enough to stop upstream work after a downstream branch no longer needs
// it, and every task consumes it before reporting to the ledger.
var errAbandoned = errors.New("runtime delivery was abandoned")

func isAbandoned(err error) bool { return errorx.Is(err, errAbandoned) }

func (t Task) Valid() bool { return t.run != nil }

// Domain returns the failure domain this task and every slot it owns report
// to. It is created before the task and outlives it, so the run can perform
// the task's Flush and Discard on the same domain after the task has joined,
// and a payload the task's own component retained still reports somewhere the
// run collects from.
func (t Task) Domain() *journal.Domain { return t.domain }

// Sealed returns the hook, if any, that must run after this task's Run span
// has ended and before another goroutine may open a span on the same domain --
// see task.Group.StartDomain. A task with nothing waiting on its own
// completion signal (a bounded edge, whose barrier is the queue's own
// WaitIdle) returns nil.
func (t Task) Sealed() func(error) { return t.sealed }

func (t Task) Run(ctx context.Context, span *journal.Span) error {
	if t.run == nil {
		return ErrBinding
	}
	return t.run(ctx, span)
}

func (t Task) Abort() {
	if t.abort != nil {
		t.abort()
	}
}

// Discard releases queued owners after every producer and consumer using the
// task has joined. It is deliberately separate from Abort: aborting wakes
// tasks, while discarding is only race-free after they have stopped.
//
// What it releases still belongs to this task's domain, so the caller does not
// name one. The lifecycle step it lands under comes from the Discard span the
// caller opens on that domain.
func (t Task) Discard() {
	if t.discard != nil {
		t.discard()
	}
}

func (t Task) Finish(ctx context.Context) error {
	if t.finish == nil {
		return nil
	}
	return t.finish(ctx)
}

func (t Task) Barrier(ctx context.Context) error {
	if t.barrier == nil {
		return nil
	}
	return t.barrier(ctx)
}

// sourceTask reuses one ownership slot for the whole stream. Anything the chain
// leaves unconsumed -- including during panic unwinding -- is released by the
// deferred Drop, so no stage needs a failure-path ownership rule.
//
// The domain is a construction argument, and binding the chain is part of
// construction. A task cannot exist holding slots that report nowhere.
func sourceTask[T any](reader flow.Reader[T], typ schema.Type[T], next delivery[T], owner *journal.Domain) Task {
	state := &sourceState[T]{reader: reader, typ: typ, next: next, site: owner.At(owner.Home())}
	state.item.Bind(typ, state.site.Reporter())
	return Task{domain: owner, finish: state.finish, run: state.run}
}

// readWithItem is one failure, not two: a Reader that failed while still
// holding the caller's slot. It wraps rather than joins so the run records it
// as the single occurrence it is, while both the sentinel and the Reader's own
// error stay findable.
type readWithItem struct{ err error }

func (e *readWithItem) Error() string        { return ErrReadWithItem.Error() + ": " + e.err.Error() }
func (e *readWithItem) Unwrap() error        { return e.err }
func (e *readWithItem) Is(target error) bool { return target == ErrReadWithItem }

type sourceState[T any] struct {
	reader flow.Reader[T]
	typ    schema.Type[T]
	next   delivery[T]
	item   flow.Item[T]
	site   *journal.Site
	eof    bool
}

func (s *sourceState[T]) run(ctx context.Context, span *journal.Span) error {
	defer s.item.Drop()
	for {
		err := s.reader.Read(ctx, &s.item)
		if errorx.Is(err, io.EOF) {
			if s.item.Valid() {
				return ErrReadWithItem
			}
			s.eof = true
			return nil
		}
		if err != nil {
			if s.item.Valid() {
				return &readWithItem{err: err}
			}
			return err
		}
		if !s.item.Valid() {
			return ErrInvalidItem
		}
		if err := s.next.Emit(ctx, &s.item); err != nil {
			if isAbandoned(err) {
				return nil
			}
			return err
		}
		// The value is finished here, not at the next Read. Releasing what the
		// chain declined at the top of the loop would let a failed release pass
		// as a read that had not happened yet, and would leave the slot full at
		// EOF, which reads as a Reader that returned an item with its EOF.
		s.item.Drop()
		if !span.Clean() {
			// The declined payload could not be released. Reading another value
			// would leak the next one the same way.
			return nil
		}
	}
}

func (s *sourceState[T]) finish(ctx context.Context) error {
	if !s.eof {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return s.next.close(ctx)
}
