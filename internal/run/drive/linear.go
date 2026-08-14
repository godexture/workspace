// The single-consumer path: a boundary sink writes items out, and a linear
// processor hands each item to the next edge.
package drive

import (
	"context"
	"errors"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/media/schema"
)

type writerDelivery[T any] struct {
	writer flow.Writer[T]
	typ    schema.Type[T]
	node   string
	scope  *journal.Scope
}

func (w *writerDelivery[T]) Own(into *flow.Item[T], value T) {
	into.Bind(w.typ, w.scope)
	into.Set(value)
}

func (w *writerDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	previous := w.scope.Enter(w.node)
	err := w.writer.Write(ctx, item)
	w.scope.Enter(previous)
	return err
}

func (*writerDelivery[T]) close(context.Context) error      { return nil }
func (w *writerDelivery[T]) bindScope(scope *journal.Scope) { w.scope = scope }

type processorDelivery[I, O any] struct {
	processor flow.Processor[I, O]
	next      delivery[O]
	typ       schema.Type[I]
	node      string
	scope     *journal.Scope
	once      sync.Once
	closeErr  error
}

func (p *processorDelivery[I, O]) Own(into *flow.Item[I], value I) {
	into.Bind(p.typ, p.scope)
	into.Set(value)
}

func (p *processorDelivery[I, O]) Emit(ctx context.Context, item *flow.Item[I]) error {
	previous := p.scope.Enter(p.node)
	err := p.processor.Process(ctx, item, p.next)
	p.scope.Enter(previous)
	return err
}

func (p *processorDelivery[I, O]) close(ctx context.Context) error {
	p.once.Do(func() {
		previous := p.scope.Enter(p.node)
		p.closeErr = errors.Join(p.processor.Flush(ctx, p.next), p.next.close(ctx))
		p.scope.Enter(previous)
	})
	return p.closeErr
}

func (p *processorDelivery[I, O]) bindScope(scope *journal.Scope) {
	p.scope = scope
	if next, ok := p.next.(scopeBinder); ok {
		next.bindScope(scope)
	}
}
