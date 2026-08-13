// The single-consumer path: a boundary sink writes items out, and a linear
// processor hands each item to the next edge.
package drive

import (
	"context"
	"errors"
	"sync"

	"github.com/godexture/godec/flow"
)

type writerDelivery[T any] struct {
	writer flow.Writer[T]
	node   string
	scope  *Scope
}

func (w *writerDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	previous := w.scope.Node()
	w.scope.set(w.node)
	err := w.writer.Write(ctx, item)
	w.scope.set(previous)
	return err
}

func (*writerDelivery[T]) close(context.Context) error { return nil }
func (w *writerDelivery[T]) bindScope(scope *Scope)    { w.scope = scope }

type processorDelivery[I, O any] struct {
	processor flow.Processor[I, O]
	next      delivery[O]
	node      string
	scope     *Scope
	once      sync.Once
	closeErr  error
}

func (p *processorDelivery[I, O]) Emit(ctx context.Context, item *flow.Item[I]) error {
	previous := p.scope.Node()
	p.scope.set(p.node)
	err := p.processor.Process(ctx, item, p.next)
	p.scope.set(previous)
	return err
}

func (p *processorDelivery[I, O]) close(ctx context.Context) error {
	p.once.Do(func() {
		previous := p.scope.Node()
		p.scope.set(p.node)
		p.closeErr = errors.Join(p.processor.Flush(ctx, p.next), p.next.close(ctx))
		p.scope.set(previous)
	})
	return p.closeErr
}

func (p *processorDelivery[I, O]) bindScope(scope *Scope) {
	p.scope = scope
	if next, ok := p.next.(scopeBinder); ok {
		next.bindScope(scope)
	}
}
