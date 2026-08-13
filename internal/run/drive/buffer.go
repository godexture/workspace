// A bounded edge decouples two operators through a queue with its own drain
// task.
package drive

import (
	"context"
	"errors"
	"io"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/schema"
)

type bufferDelivery[T any] struct {
	queue *queue.Queue[T]
}

// Emit moves the cell into the queue, which owns it until Pop succeeds. A
// rejected push never empties the cell, so the producer stays responsible and
// the payload is never in two places.
func (b *bufferDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	if item == nil || !item.Valid() {
		return ErrInvalidItem
	}
	return b.queue.Push(ctx, item)
}

func (b *bufferDelivery[T]) close(context.Context) error {
	b.queue.Close()
	return nil
}

func bufferFactory[T any](traits schema.Traits[T]) func(queue.Limit, Link) (Link, Task, error) {
	return func(limit queue.Limit, next Link) (Link, Task, error) {
		target, err := deliveryOf[T](next)
		if err != nil {
			return Link{}, Task{}, err
		}
		edge, err := queue.New(limit, queueTraits(traits))
		if err != nil {
			return Link{}, Task{}, err
		}
		task := Task{
			close:   edge.Close,
			discard: func() { edge.Drain() },
			barrier: edge.WaitIdle,
			run: func(ctx context.Context) error {
				defer edge.Drain()
				var item flow.Item[T]
				defer item.Drop()
				for {
					err := edge.Pop(ctx, &item)
					if errors.Is(err, io.EOF) {
						return target.close(ctx)
					}
					if err != nil {
						return err
					}
					emitErr := target.Emit(ctx, &item)
					edge.Complete()
					if emitErr != nil {
						return emitErr
					}
				}
			},
		}
		return linkOf[T](&bufferDelivery[T]{queue: edge}), task, nil
	}
}

// queueTraits hands the edge-local measurements to the queue. Releasing stays
// with the cells the queue stores.
func queueTraits[T any](traits schema.Traits[T]) queue.Traits[T] {
	return queue.Traits[T]{Size: traits.Size, Time: traits.Time}
}
