// Fan-out duplicates one item across branches, forking owned payloads so a
// branch cannot observe a sibling.
package drive

import (
	"context"
	"errors"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/release"
	"github.com/godexture/godec/media/schema"
)

type fanoutDelivery[T any] struct {
	outputs  []delivery[T]
	branches []flow.Item[T]
	typ      schema.Type[T]
	once     sync.Once
	closeErr error
}

// Own binds the caller's slot to the first output, which is the edge the
// payload the caller is about to fill would reach without the fork.
func (f *fanoutDelivery[T]) Own(into *flow.Item[T], value T) { f.outputs[0].Own(into, value) }

func fanoutFactory[T any](typ schema.Type[T]) func([]Link) (Link, error) {
	traits := typ.Traits()
	return func(links []Link) (Link, error) {
		if len(links) == 0 {
			return Link{}, ErrLink
		}
		if len(links) == 1 {
			if _, err := deliveryOf[T](links[0]); err != nil {
				return Link{}, err
			}
			return links[0], nil
		}
		if traits.Drop != nil && traits.Fork == nil {
			return Link{}, ErrForkTrait
		}
		outputs := make([]delivery[T], len(links))
		for index, link := range links {
			output, err := deliveryOf[T](link)
			if err != nil {
				return Link{}, err
			}
			outputs[index] = output
		}
		return linkOf[T](&fanoutDelivery[T]{
			outputs:  outputs,
			branches: make([]flow.Item[T], len(outputs)-1),
			typ:      typ,
		}), nil
	}
}

// Emit forks one owner per extra output and hands the original to the last
// one. Branch cells are reused across items, and the deferred release covers
// every failure and panic path without per-branch bookkeeping.
func (f *fanoutDelivery[T]) Emit(ctx context.Context, item *flow.Item[T]) (err error) {
	// Every branch a consumer did not take has to be released. A fan-out holds
	// several owners of one payload, so stopping at the first broken Drop would
	// strand the rest; each branch slot reports its own failure to the task's
	// journal and none of them can interrupt the others.
	defer release.All(f.branches)
	for index := range f.branches {
		if !item.Fork(&f.branches[index]) {
			return ErrInvalidItem
		}
	}
	for index := range f.branches {
		if err := f.outputs[index].Emit(ctx, &f.branches[index]); err != nil {
			return err
		}
	}
	return f.outputs[len(f.outputs)-1].Emit(ctx, item)
}

func (f *fanoutDelivery[T]) close(ctx context.Context) error {
	f.once.Do(func() {
		problems := make([]error, 0, len(f.outputs))
		for _, output := range f.outputs {
			if err := output.close(ctx); err != nil {
				problems = append(problems, err)
			}
		}
		f.closeErr = errors.Join(problems...)
	})
	return f.closeErr
}

// bindScope binds the branch slots to the task that drives this fan-out: they
// are its slots, and a branch nobody took is released by it.
func (f *fanoutDelivery[T]) bindScope(scope *journal.Scope) {
	for index := range f.branches {
		f.branches[index].Bind(f.typ, scope)
	}
	for _, output := range f.outputs {
		if value, ok := output.(scopeBinder); ok {
			value.bindScope(scope)
		}
	}
}
