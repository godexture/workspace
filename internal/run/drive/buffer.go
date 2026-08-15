// A bounded edge decouples two operators through a queue with its own drain
// task.
package drive

import (
	"context"
	"errors"
	"io"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/schema"
)

// bufferDelivery is the producer's end of the edge. Its scope is the producer's
// task, because that is who fills the slots it hands out; the ring's own slots
// belong to the drain task and are bound separately.
type bufferDelivery[T any] struct {
	queue *queue.Queue[T]
	typ   schema.Type[T]
	scope *journal.Scope
}

func (b *bufferDelivery[T]) Own(into *flow.Item[T], value T) {
	into.Bind(b.typ, b.scope)
	into.Set(value)
}

// Emit moves the payload into the queue, which owns it until Pop succeeds. A
// rejected push never empties the slot, so the producer stays responsible and
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

// bindScope stops here. The scope below this edge belongs to the drain task,
// not to the producer, and the two must not be confused: a release the drain
// task cannot perform is its failure, not the producer's.
func (b *bufferDelivery[T]) bindScope(scope *journal.Scope) { b.scope = scope }

func bufferFactory[T any](typ schema.Type[T]) func(queue.Limit, Link) (Link, Task, error) {
	return func(limit queue.Limit, next Link) (Link, Task, error) {
		target, err := deliveryOf[T](next)
		if err != nil {
			return Link{}, Task{}, err
		}
		edge, err := queue.New(limit, typ)
		if err != nil {
			return Link{}, Task{}, err
		}
		state := &bufferState[T]{queue: edge, target: target, typ: typ}
		state.bindScope(journal.New(journal.Run, ""))
		task := Task{
			close:   edge.Close,
			discard: state.discard,
			barrier: edge.WaitIdle,
			bind:    state.bindScope,
			run:     state.run,
		}
		return linkOf[T](&bufferDelivery[T]{queue: edge, typ: typ}), task, nil
	}
}

type bufferState[T any] struct {
	queue  *queue.Queue[T]
	target delivery[T]
	typ    schema.Type[T]
	item   flow.Item[T]
	scope  *journal.Scope
}

// bindScope claims the edge for the drain task: the ring's slots and the one
// slot the loop carries are all released by this task, so they all report here.
func (b *bufferState[T]) bindScope(scope *journal.Scope) {
	b.scope = scope
	b.item.Bind(b.typ, scope)
	b.queue.Bind(scope)
}

// discard runs after the task has joined, so what it releases belongs to
// whoever is cleaning up rather than to the task that stopped.
func (b *bufferState[T]) discard(into flow.Reporter) { b.queue.Drain(into) }

func (b *bufferState[T]) run(ctx context.Context) error {
	// holding records the one popped value the loop still owes the edge, so the
	// returning path settles it once for the task rather than a defer per item.
	// Every exit that reaches it left a value unfinished, so it is abandoned
	// rather than completed and the edge does not report a quiescence it never
	// reached.
	holding := false
	defer func() {
		if holding {
			b.queue.Abandon()
		}
		// The loop leaves whatever it had not emitted in the ring, so draining
		// it is part of returning. This is still the task's own cleanup, and
		// the ring's slots report to the task's journal.
		b.queue.Drain(b.scope)
	}()
	defer b.item.Drop()
	for {
		err := b.queue.Pop(ctx, &b.item)
		if errors.Is(err, io.EOF) {
			// Closing what this edge feeds is still this goroutine's own act:
			// no other goroutine can perform it without racing this one, and
			// none needs to. The barrier only promises the ring is empty,
			// never that this call already happened, so nothing outside this
			// task may depend on it having run before the task itself has
			// joined.
			//
			// It is still a genuine Flush, though, the same lifecycle step a
			// direct chain reaches through Host's own Execution.Finish. Rather
			// than open a second journal for it -- which would recreate the
			// cross-goroutine read this design avoids, since nothing lets
			// Host know this goroutine has reached it -- this journal
			// relabels itself for the rest of its life. Task and Seq keep
			// every event's identity regardless, and the ring is provably
			// empty on this path (Pop only returns EOF once count reaches
			// zero, and Close makes that permanent), so nothing this task
			// still owns crosses the relabeling into the wrong operation.
			b.scope.EnterOperation(journal.Flush)
			return b.target.close(ctx)
		}
		if err != nil {
			return err
		}
		// The value is finished once the consumer has had its chance at it and
		// whatever the consumer declined has been released. A failure at either
		// point -- an error, a panic, or a declared Drop that fails -- leaves
		// the loop still holding it, so the exit abandons it.
		holding = true
		if err := b.target.Emit(ctx, &b.item); err != nil {
			return err
		}
		b.item.Drop()
		if !b.scope.Clean() {
			// The payload the consumer declined could not be released. Going
			// back for another value would leak the next one the same way, and
			// this one was never finished, so the exit abandons it.
			return nil
		}
		b.queue.Complete()
		holding = false
	}
}
