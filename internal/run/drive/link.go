// Link is the typed edge between two operators, and Scope names the node an
// edge belongs to for diagnostics.
package drive

import (
	"context"
	"fmt"
	"reflect"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
)

type closer interface {
	close(context.Context) error
}

// A delivery chain is bound to the journal of the task that drives it, which is
// also the failure domain of every ownership slot that task owns.
type scopeBinder interface{ bindScope(*journal.Scope) }

type delivery[T any] interface {
	flow.Emitter[T]
	closer
}

// Link is an opaque typed delivery endpoint used only while Program is
// specialized. Its payload never becomes any during delivery.
type Link struct {
	payload reflect.Type
	value   any
}

func (l Link) Valid() bool { return l.payload != nil && l.value != nil }

func (l Link) Close(ctx context.Context) error {
	if !l.Valid() {
		return ErrLink
	}
	return l.value.(closer).close(ctx)
}

func (l Link) BindScope(scope *journal.Scope) {
	if !l.Valid() {
		return
	}
	if value, ok := l.value.(scopeBinder); ok {
		value.bindScope(scope)
	}
}

func linkOf[T any](value delivery[T]) Link {
	return Link{payload: reflect.TypeFor[T](), value: value}
}

func deliveryOf[T any](link Link) (delivery[T], error) {
	if link.payload != reflect.TypeFor[T]() {
		return nil, fmt.Errorf("%w: want %s, got %v", ErrLink, reflect.TypeFor[T](), link.payload)
	}
	value, ok := link.value.(delivery[T])
	if !ok {
		return nil, fmt.Errorf("%w: link contains %T", ErrLink, link.value)
	}
	return value, nil
}

// Task is a top-level execution loop. Host runs it through the tracked task
// group, which owns panic recovery, cancellation, and join.
