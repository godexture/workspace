// Task is one runnable unit the execution island schedules: a source pump or
// a fan-in join.
package drive

import (
	"context"
	"errors"
	"io"

	"github.com/godexture/godec/flow"
)

type Task struct {
	run     func(context.Context) error
	barrier func(context.Context) error
	finish  func(context.Context) error
	close   func()
	discard func() error
	bind    func(*Scope)
}

func (t Task) Valid() bool { return t.run != nil }

func (t Task) Run(ctx context.Context) error {
	if t.run == nil {
		return ErrBinding
	}
	return t.run(ctx)
}

func (t Task) Close() {
	if t.close != nil {
		t.close()
	}
}

// Discard releases queued owners after every producer and consumer using the
// task has joined. It is deliberately separate from Close: closing wakes
// tasks, while discarding is only race-free after they have stopped.
//
// It reports rather than panics. Discard is the last cleanup on paths that
// have already lost their recovery boundary, and a declared Drop that panics
// there must not take the remaining owners with it.
func (t Task) Discard() error {
	if t.discard == nil {
		return nil
	}
	return t.discard()
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

func (t Task) BindScope(scope *Scope) {
	if t.bind != nil {
		t.bind(scope)
	}
}

// sourceTask reuses one ownership cell for the whole stream. Anything the
// chain leaves unconsumed — including during panic unwinding — is released by
// the deferred Drop, so no stage needs a failure-path ownership rule.
func sourceTask[T any](reader flow.Reader[T], next delivery[T]) Task {
	return Task{finish: next.close, run: func(ctx context.Context) error {
		var item flow.Item[T]
		defer item.Drop()
		for {
			err := reader.Read(ctx, &item)
			if errors.Is(err, io.EOF) {
				if item.Valid() {
					return ErrReadWithItem
				}
				return nil
			}
			if err != nil {
				if item.Valid() {
					return errors.Join(ErrReadWithItem, err)
				}
				return err
			}
			if !item.Valid() {
				return ErrInvalidItem
			}
			if err := next.Emit(ctx, &item); err != nil {
				return err
			}
		}
	}}
}
