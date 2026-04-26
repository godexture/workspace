package pipeline

import (
	"context"
	"io"
)

type ChanEdge[T any] struct {
	ch chan T
}

func NewChanEdge[T any](bufferSize int) *ChanEdge[T] {
	return &ChanEdge[T]{ch: make(chan T, bufferSize)}
}

func (e *ChanEdge[T]) Push(ctx context.Context, item T) error {
	select {
	case e.ch <- item:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *ChanEdge[T]) Pull(ctx context.Context) (T, error) {
	select {
	case item, ok := <-e.ch:
		if !ok {
			var zero T
			return zero, io.EOF
		}
		return item, nil
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

func (e *ChanEdge[T]) Close() {
	close(e.ch)
}
