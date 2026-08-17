// The single-consumer path: a boundary sink writes items out, and a linear
// processor hands each item to the next edge.
package drive

import (
	"context"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/cancel"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/media/schema"
)

type writerDelivery[T any] struct {
	writer flow.Writer[T]
	typ    schema.Type[T]
	node   string
	site   *journal.Site
}

func (w *writerDelivery[T]) Own(into *flow.Item[T], value T) {
	into.Bind(w.typ, w.site.Reporter())
	into.Set(value)
}

func (w *writerDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) error {
	return performDelivery(ctx, w.site, func() error {
		return w.writer.Write(ctx, item)
	})
}

func (*writerDelivery[T]) close(context.Context) error { return nil }

func (*writerDelivery[T]) prepareClose(context.Context) {}

func (w *writerDelivery[T]) bindDomain(domain *journal.Domain) { w.site = domain.At(w.node) }
func (w *writerDelivery[T]) bound() bool                       { return w.site != nil }

type processorDelivery[I, O any] struct {
	processor flow.Processor[I, O]
	next      delivery[O]
	typ       schema.Type[I]
	node      string
	site      *journal.Site
	once      sync.Once
	closeErr  error
}

func (p *processorDelivery[I, O]) Own(into *flow.Item[I], value I) {
	into.Bind(p.typ, p.site.Reporter())
	into.Set(value)
}

func (p *processorDelivery[I, O]) Emit(ctx context.Context, item *flow.Item[I]) error {
	return performDelivery(ctx, p.site, func() error {
		return p.processor.Process(ctx, item, p.next)
	})
}

// performDelivery gives a component callback its node-local panic and error
// boundary. A callback that only reports the context's cancellation is a
// consequence of the failure that stopped the path, not an independent work
// occurrence. Keep the original error for the queue/task boundary while
// letting Site.Perform remain the single place that records genuine failures.
func performDelivery(ctx context.Context, site *journal.Site, work func() error) error {
	var returned error
	cause := site.Perform(func() error {
		returned = work()
		if cancellationEcho(ctx, returned) {
			return nil
		}
		return returned
	})
	if cause != nil {
		return cause
	}
	return returned
}

func cancellationEcho(ctx context.Context, err error) bool {
	return cancel.Normalize(ctx, err) != nil
}

// close flushes this component and then closes what it feeds.
//
// Each is its own failure and each is recorded as it happens. Joining them
// into one error before the ledger saw them would make two components that
// failed independently a single event, which no consumer could take apart
// again -- and Result.Secondary exists precisely to report them separately.
// What comes back is only the first, as a reference, so a caller has something
// to stop on without it being a second account of what went wrong.
func (p *processorDelivery[I, O]) close(ctx context.Context) error {
	p.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		if cause := context.Cause(ctx); cause != nil {
			p.closeErr = cause
			return
		}
		flushed := p.site.Perform(func() error {
			err := p.processor.Flush(ctx, p.next)
			if isAbandoned(err) {
				return nil
			}
			return err
		})
		p.closeErr = firstFailure(flushed, p.next.close(ctx))
	})
	return p.closeErr
}

// prepareClose records the phase context through a processor without sealing
// anything. Its Flush may emit delayed output, but the bounded descendant can
// safely prepare its context before that output is produced.
func (p *processorDelivery[I, O]) prepareClose(ctx context.Context) {
	if value, ok := p.next.(interface{ prepareClose(context.Context) }); ok {
		value.prepareClose(ctx)
	}
}

// firstFailure returns the earliest failure of a sequence that has already
// recorded each of its own.
func firstFailure(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func (p *processorDelivery[I, O]) bindDomain(domain *journal.Domain) {
	p.site = domain.At(p.node)
	if next, ok := p.next.(domainBinder); ok {
		next.bindDomain(domain)
	}
}

func (p *processorDelivery[I, O]) bound() bool {
	if p.site == nil {
		return false
	}
	next, ok := p.next.(domainBinder)
	return !ok || next.bound()
}
