// A bounded edge decouples two operators through a queue with its own drain
// task.
package drive

import (
	"context"
	"io"
	"sync/atomic"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/errorx"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/schema"
)

// bufferDelivery is the producer's end of the edge. Its site belongs to the
// producer's task, because that is who fills the slots it hands out; the
// ring's own slots belong to the drain task and are bound separately.
type bufferDelivery[T any] struct {
	queue *queue.Queue[T]
	typ   schema.Type[T]
	node  string
	site  *journal.Site
	state *bufferState[T]
}

func (b *bufferDelivery[T]) Own(into *flow.Item[T], value T) {
	into.Bind(b.typ, b.site.Reporter())
	into.Set(value)
}

// Emit moves the payload into the queue, which owns it until Pop succeeds. A
// rejected push never empties the slot, so the producer stays responsible and
// the payload is never in two places.
//
// Finding the edge closed is not a failure of this producer. The consumer end
// closes because something already stopped, and the producer is meeting the
// consequence of that rather than failing on its own, so it names the event
// that stopped the run instead of reporting a second, unrelated-looking
// failure -- which would also make what a run reports depend on how far a
// producer got before it noticed.
func (b *bufferDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	if item == nil || !item.Valid() {
		return ErrInvalidItem
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := b.queue.Push(ctx, item)
	if errorx.Is(err, queue.ErrAbandoned) {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		if cause := b.site.Domain().Ledger().Stopped(); cause != nil {
			return cause
		}
		return errAbandoned
	}
	if errorx.Is(err, queue.ErrClosed) {
		if cause := b.site.Domain().Ledger().Stopped(); cause != nil {
			return cause
		}
	}
	return err
}

// prepareClose records the phase context before Seal makes EOF observable to
// the drain task. The task may still be blocked in Pop with the data-group
// context; the state switches that one operation to this context when it
// wakes on the seal.
func (b *bufferDelivery[T]) prepareClose(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if b.state != nil {
		b.state.finish.Store(&closeContext{ctx: ctx})
	}
}

func (b *bufferDelivery[T]) close(ctx context.Context) error {
	b.prepareClose(ctx)
	b.queue.Seal()
	return nil
}

// bindDomain stops here. The domain below this edge belongs to the drain task,
// not to the producer, and the two must not be confused: a release the drain
// task cannot perform is its failure, not the producer's.
func (b *bufferDelivery[T]) bindDomain(domain *journal.Domain) { b.site = domain.At(b.node) }
func (b *bufferDelivery[T]) bound() bool                       { return b.site != nil }

func bufferFactory[T any](typ schema.Type[T]) func(queue.Limit, Link, *journal.Domain) (Link, Task, error) {
	return func(limit queue.Limit, next Link, owner *journal.Domain) (Link, Task, error) {
		target, err := deliveryOf[T](next)
		if err != nil {
			return Link{}, Task{}, err
		}
		site := owner.At(owner.Home())
		edge, err := queue.New(limit, typ, site.Reporter())
		if err != nil {
			return Link{}, Task{}, err
		}
		state := &bufferState[T]{queue: edge, target: target, typ: typ, site: site}
		state.item.Bind(typ, site.Reporter())
		delivery := &bufferDelivery[T]{queue: edge, typ: typ, node: owner.Home(), state: state}
		task := Task{
			domain:  owner,
			abort:   edge.Abort,
			discard: state.discard,
			barrier: edge.WaitIdle,
			run:     state.run,
		}
		return linkOf[T](delivery), task, nil
	}
}

type bufferState[T any] struct {
	queue  *queue.Queue[T]
	target delivery[T]
	typ    schema.Type[T]
	item   flow.Item[T]
	site   *journal.Site
	finish atomic.Pointer[closeContext]
}

type closeContext struct{ ctx context.Context }

func (b *bufferState[T]) closeContext() context.Context {
	value := b.finish.Load()
	if value == nil {
		return nil
	}
	return value.ctx
}

// discard runs after the task has joined. What it releases is still the edge's
// own, so the caller does not rename it; the Discard span the caller opened on
// this domain is what places it in the run's lifecycle.
func (b *bufferState[T]) discard() { b.queue.Drain() }

func (b *bufferState[T]) run(ctx context.Context, span *journal.Span) error {
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
		// the ring's slots report to the task's own domain.
		b.queue.Drain()
	}()
	defer b.item.Drop()
	for {
		workCtx := ctx
		prepared := b.closeContext()
		if prepared != nil {
			workCtx = prepared
		}
		err := b.queue.Pop(workCtx, &b.item)
		if err != nil {
			// Seal wakes a Pop that started before prepareClose. If a peer's
			// Flush canceled the data-group context in that window, retry with
			// the prepared phase context; an external cancellation is still
			// visible through that context and stops the edge.
			if finishCtx := b.closeContext(); finishCtx != nil && prepared == nil {
				if cause := context.Cause(ctx); cause != nil && errorx.Is(err, cause) {
					if phaseCause := context.Cause(finishCtx); phaseCause != nil {
						return phaseCause
					}
					continue
				}
			}
		}
		if errorx.Is(err, io.EOF) {
			// Closing what this edge feeds is still this goroutine's own act:
			// no other goroutine can perform it without racing this one, and
			// none needs to. The barrier only promises the ring is empty,
			// never that this call already happened, so nothing outside this
			// task may depend on it having run before the task itself has
			// joined.
			//
			// It is a genuine Flush, though, the same lifecycle step a direct
			// chain reaches through the run's own Execution.Finish. So it gets
			// a Flush span, opened and ended by this goroutine inside the Run
			// span it is still executing. Nothing is relabeled and no second
			// goroutine comes near this domain; the failures this close
			// produces simply name the operation they belong to. Its cause
			// returns as this task's error, and the ledger recognizes it as
			// the event it already holds rather than recording it twice.
			flushCtx := workCtx
			if finishCtx := b.closeContext(); finishCtx != nil {
				flushCtx = finishCtx
			}
			if cause := context.Cause(flushCtx); cause != nil {
				return cause
			}
			return b.site.Domain().Perform(journal.Flush, func(*journal.Span) error {
				return b.target.close(flushCtx)
			})
		}
		if errorx.Is(err, queue.ErrAbandoned) {
			return nil
		}
		if err != nil {
			return err
		}
		// The value is finished once the consumer has had its chance at it and
		// whatever the consumer declined has been released. A failure at either
		// point -- an error, a panic, or a declared Drop that fails -- leaves
		// the loop still holding it, so the exit abandons it.
		holding = true
		if err := b.target.Emit(workCtx, &b.item); err != nil {
			if isAbandoned(err) {
				b.item.Drop()
				if !span.Clean() {
					return nil
				}
				b.queue.Complete()
				holding = false
				b.queue.Abort()
				return nil
			}
			return err
		}
		b.item.Drop()
		if !span.Clean() {
			// The payload the consumer declined could not be released. Going
			// back for another value would leak the next one the same way, and
			// this one was never finished, so the exit abandons it.
			return nil
		}
		b.queue.Complete()
		holding = false
	}
}
