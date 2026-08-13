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
	queue  *queue.Queue[T]
	traits schema.Traits[T]
}

// Emit hands the value to the queue, which owns it until Pop succeeds. A
// rejected push never stores it, so the cell takes it back and the producer
// stays responsible. Traits belong to the edge, not to each value, so the
// queue stores values alone and no copyable ownership token exists.
func (b *bufferDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	value, ok := item.Detach()
	if !ok {
		return ErrInvalidItem
	}
	if err := b.queue.Push(ctx, value); err != nil {
		item.SetWithTraits(value, b.traits.Fork, b.traits.Drop)
		return err
	}
	return nil
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
					value, err := edge.Pop(ctx)
					if errors.Is(err, io.EOF) {
						return target.close(ctx)
					}
					if err != nil {
						return err
					}
					item.SetWithTraits(value, traits.Fork, traits.Drop)
					emitErr := target.Emit(ctx, &item)
					edge.Complete()
					if emitErr != nil {
						return emitErr
					}
				}
			},
		}
		return linkOf[T](&bufferDelivery[T]{queue: edge, traits: traits}), task, nil
	}
}

// queueTraits hands the schema traits straight to the queue, which owns every
// value it holds and releases the remainder when drained.
func queueTraits[T any](traits schema.Traits[T]) queue.Traits[T] {
	return queue.Traits[T]{Drop: traits.Drop, Size: traits.Size, Time: traits.Time}
}
