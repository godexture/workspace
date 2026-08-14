// Task is one runnable unit the execution island schedules: a source pump or
// a fan-in join.
package drive

import (
	"context"
	"errors"
	"io"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/media/schema"
)

type Task struct {
	run     func(context.Context) error
	barrier func(context.Context) error
	finish  func(context.Context) error
	close   func()
	discard func(flow.Reporter)
	bind    func(*journal.Scope)
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
// task has joined. It is deliberately separate from Close: closing wakes tasks,
// while discarding is only race-free after they have stopped.
//
// The domain is named by the caller because the task's own journal is sealed by
// then. What is released here belongs to whoever is cleaning up.
func (t Task) Discard(into flow.Reporter) {
	if t.discard != nil {
		t.discard(into)
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

func (t Task) BindScope(scope *journal.Scope) {
	if t.bind != nil {
		t.bind(scope)
	}
}

// sourceTask reuses one ownership slot for the whole stream. Anything the chain
// leaves unconsumed -- including during panic unwinding -- is released by the
// deferred Drop, so no stage needs a failure-path ownership rule.
func sourceTask[T any](reader flow.Reader[T], typ schema.Type[T], next delivery[T]) Task {
	state := &sourceState[T]{reader: reader, typ: typ, next: next}
	state.bindScope(journal.NewScope(""))
	return Task{finish: next.close, bind: state.bindScope, run: state.run}
}

type sourceState[T any] struct {
	reader flow.Reader[T]
	typ    schema.Type[T]
	next   delivery[T]
	item   flow.Item[T]
}

func (s *sourceState[T]) bindScope(scope *journal.Scope) { s.item.Bind(s.typ, scope) }

func (s *sourceState[T]) run(ctx context.Context) error {
	defer s.item.Drop()
	for {
		err := s.reader.Read(ctx, &s.item)
		if errors.Is(err, io.EOF) {
			if s.item.Valid() {
				return ErrReadWithItem
			}
			return nil
		}
		if err != nil {
			if s.item.Valid() {
				return errors.Join(ErrReadWithItem, err)
			}
			return err
		}
		if !s.item.Valid() {
			return ErrInvalidItem
		}
		if err := s.next.Emit(ctx, &s.item); err != nil {
			return err
		}
	}
}
