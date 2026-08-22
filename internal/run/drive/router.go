package drive

import (
	"context"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/media/schema"
)

// routerDelivery is a linear input delivery with a fixed, dense table of
// output routes. It never forks between routes: each route owns its own
// fan-out, if the compiled graph needs one.
type routerDelivery[I, O any] struct {
	router   flow.Router[I, O]
	routes   []delivery[O]
	typ      schema.Type[I]
	node     string
	site     *journal.Site
	once     sync.Once
	closeErr error
}

func (r *routerDelivery[I, O]) Own(into *flow.Item[I], value I) {
	into.Bind(r.typ, r.site.Reporter())
	into.Set(value)
}

func (r *routerDelivery[I, O]) Emit(ctx context.Context, item *flow.Item[I]) error {
	return performDelivery(ctx, r.site, func() error {
		return r.router.Process(ctx, item, r)
	})
}

func (r *routerDelivery[I, O]) Route(ordinal int) (flow.Emitter[O], bool) {
	if ordinal < 0 || ordinal >= len(r.routes) {
		return nil, false
	}
	return r.routes[ordinal], true
}

func (r *routerDelivery[I, O]) close(ctx context.Context) error {
	r.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		if cause := context.Cause(ctx); cause != nil {
			r.closeErr = cause
			return
		}
		flushed := r.site.Perform(func() error {
			err := r.router.Flush(ctx, r)
			if isAbandoned(err) {
				return nil
			}
			return err
		})
		r.closeErr = flushed
		for _, route := range r.routes {
			r.closeErr = firstFailure(r.closeErr, route.close(ctx))
		}
	})
	return r.closeErr
}

func (r *routerDelivery[I, O]) prepareClose(ctx context.Context) {
	for _, route := range r.routes {
		if value, ok := route.(interface{ prepareClose(context.Context) }); ok {
			value.prepareClose(ctx)
		}
	}
}

func (r *routerDelivery[I, O]) bindDomain(domain *journal.Domain) {
	r.site = domain.At(r.node)
	for _, route := range r.routes {
		if value, ok := route.(domainBinder); ok {
			value.bindDomain(domain)
		}
	}
}

func (r *routerDelivery[I, O]) bound() bool {
	if r.site == nil {
		return false
	}
	for _, route := range r.routes {
		if value, ok := route.(domainBinder); ok && !value.bound() {
			return false
		}
	}
	return true
}
