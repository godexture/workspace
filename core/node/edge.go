package node

import "context"

type Edge[T any] interface {
	Push(ctx context.Context, item T) error
	Pull(ctx context.Context) (T, error)
	Close()
}
