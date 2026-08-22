package drive

import (
	"context"
	"io"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/errorx"
	"github.com/godexture/godec/internal/journal"
)

// routedSourceTask drives a push source. Its reader may emit more than one
// item per Read call, but every successful call must make progress.
func routedSourceTask[T any](reader flow.RoutedReader[T], routes []delivery[T], owner *journal.Domain) Task {
	state := &routedSourceState[T]{reader: reader, routes: routes}
	state.emitters = make([]routedSourceEmitter[T], len(routes))
	for index := range state.emitters {
		state.emitters[index] = routedSourceEmitter[T]{state: state, target: routes[index]}
	}
	return Task{domain: owner, finish: state.finish, run: state.run}
}

type routedSourceState[T any] struct {
	reader    flow.RoutedReader[T]
	routes    []delivery[T]
	emitters  []routedSourceEmitter[T]
	eof       bool
	emitted   int
	abandoned bool
	once      sync.Once
	closeErr  error
}

func (s *routedSourceState[T]) Route(ordinal int) (flow.Emitter[T], bool) {
	if ordinal < 0 || ordinal >= len(s.emitters) {
		return nil, false
	}
	return &s.emitters[ordinal], true
}

func (s *routedSourceState[T]) run(ctx context.Context, span *journal.Span) error {
	for {
		s.emitted = 0
		s.abandoned = false
		err := s.reader.Read(ctx, s)
		if s.abandoned || isAbandoned(err) {
			return nil
		}
		if errorx.Is(err, io.EOF) {
			if s.emitted != 0 {
				return ErrReadWithItem
			}
			s.eof = true
			return nil
		}
		if err != nil {
			if s.emitted != 0 {
				return &readWithItem{err: err}
			}
			return err
		}
		if s.emitted == 0 {
			return ErrInvalidItem
		}
		if !span.Clean() {
			return nil
		}
	}
}

func (s *routedSourceState[T]) finish(ctx context.Context) error {
	if !s.eof {
		return nil
	}
	s.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		if cause := context.Cause(ctx); cause != nil {
			s.closeErr = cause
			return
		}
		for _, route := range s.routes {
			if err := route.close(ctx); !isAbandoned(err) {
				s.closeErr = firstFailure(s.closeErr, err)
			}
		}
	})
	return s.closeErr
}

type routedSourceEmitter[T any] struct {
	state  *routedSourceState[T]
	target delivery[T]
}

func (e *routedSourceEmitter[T]) Own(into *flow.Item[T], value T) {
	e.target.Own(into, value)
}

func (e *routedSourceEmitter[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	err := e.target.Emit(ctx, item)
	if err == nil {
		e.state.emitted++
	} else if isAbandoned(err) {
		e.state.abandoned = true
	}
	return err
}
