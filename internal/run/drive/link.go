// Link is the typed edge between two operators, and binding one puts every
// ownership slot below it in the failure domain of the task that drives it.
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

// A delivery chain belongs to the failure domain of the task that drives it,
// which is also the domain of every ownership slot that task owns. Binding
// stops at a bounded edge: what is below it is the drain task's, and a release
// the drain task cannot perform is its failure rather than the producer's.
type domainBinder interface {
	bindDomain(*journal.Domain)
	bound() bool
}

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

// bind is called by the constructor of the task that will drive this chain,
// with the domain that task owns. Nothing else binds a chain, so there is no
// separate step to forget and no anonymous domain for an unbound chain to fall
// back on.
func (l Link) bind(domain *journal.Domain) {
	if !l.Valid() {
		return
	}
	if value, ok := l.value.(domainBinder); ok {
		value.bindDomain(domain)
	}
}

// Bound reports whether this endpoint has been given the failure domain it
// reports releases to. Build checks it once, so a chain that reached the item
// path without a domain is a topology error rather than a run whose release
// failures go nowhere.
func (l Link) Bound() bool {
	if !l.Valid() {
		return false
	}
	value, ok := l.value.(domainBinder)
	return !ok || value.bound()
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
