// Observation counts items and bytes on an edge without changing what flows
// through it.
package drive

import (
	"context"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/observe"
	"github.com/godexture/godec/media/schema"
)

type observedDelivery[T any] struct {
	next  delivery[T]
	local *observe.Local
	size  func(T) int
	time  func(T) (int64, bool)
}

func (o *observedDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	var bytes uint64
	if o.size != nil {
		if value := o.size(item.Value()); value > 0 {
			bytes = uint64(value)
		}
	}
	var media int64
	var timed bool
	if o.time != nil {
		media, timed = o.time(item.Value())
	}
	if err := o.next.Emit(ctx, item); err != nil {
		return err
	}
	o.local.Add(bytes, media, timed)
	return nil
}

func (o *observedDelivery[T]) close(ctx context.Context) error {
	err := o.next.close(ctx)
	o.local.Flush()
	return err
}

func (o *observedDelivery[T]) bindScope(scope *Scope) {
	if next, ok := o.next.(scopeBinder); ok {
		next.bindScope(scope)
	}
}

func observeFactory[T any](traits schema.Traits[T]) func(Link, *observe.Local) (Link, error) {
	return func(next Link, local *observe.Local) (Link, error) {
		target, err := deliveryOf[T](next)
		if err != nil {
			return Link{}, err
		}
		result := &observedDelivery[T]{next: target, local: local}
		if local.Detailed() {
			result.size = traits.Size
			result.time = traits.Time
		}
		return linkOf[T](result), nil
	}
}
