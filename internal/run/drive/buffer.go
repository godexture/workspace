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
			close: edge.Close,
			discard: func() error {
				_, err := edge.Drain()
				return err
			},
			barrier: edge.WaitIdle,
			run: func(ctx context.Context) (err error) {
				// holding records the one popped value the loop still owes the
				// edge, so the returning path settles it once for the task
				// rather than a defer per item. Every exit that reaches it left
				// a value unfinished, so it is abandoned rather than completed
				// and the edge does not report a quiescence it never reached.
				//
				// The loop also leaves whatever it had not emitted in the ring,
				// so draining it is part of returning, and a payload that cannot
				// be released is part of the answer.
				holding := false
				defer func() {
					if holding {
						edge.Abandon()
					}
					_, drainErr := edge.Drain()
					err = errors.Join(err, drainErr)
				}()
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
					// The value is finished once the consumer has had its
					// chance at it and whatever the consumer declined has been
					// released. A failure at either point -- an error, a panic,
					// or a declared Drop that panics -- leaves the loop still
					// holding it, so the exit abandons it.
					holding = true
					if err := target.Emit(ctx, &item); err != nil {
						return err
					}
					item.Drop()
					edge.Complete()
					holding = false
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
