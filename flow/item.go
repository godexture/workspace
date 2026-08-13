package flow

import (
	"errors"

	"github.com/godexture/godec/media/schema"
)

// ErrTransfer reports a linear hand-off whose source, target, or conversion is
// missing.
var ErrTransfer = errors.New("flow item transfer is invalid")

// noCopy makes `go vet` reject copies of a value that carries a release
// obligation. Constructing and returning a cell stays legal; aliasing one,
// appending it to a container, ranging over it by value, or sending it across
// a channel does not.
type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// Item is one owned value in transit.
//
// An Item is a cell, not a value: it is always passed as a pointer, and the
// first Drop or Detach releases or removes it while every later one does
// nothing. That
// single rule replaces per-call ownership protocols. A stage creates or
// receives a cell, defers Drop, and passes the pointer on; whoever consumes it
// wins, and if nobody does, the deferred Drop releases it. Success and failure
// need no distinction, because a consumed cell is already empty when the
// deferred Drop runs.
//
// Fork is the only way to obtain a second owner of the same logical item, so
// fan-out is the only place that retains.
type Item[T any] struct {
	_     noCopy
	value T
	fork  func(T) T
	drop  func(T)
	valid bool
}

// NewItem takes ownership of value under the schema's traits. An invalid
// schema cannot describe ownership, so the value is released rather than
// silently retained by a cell nobody can consume.
func NewItem[T any](value T, typ schema.Type[T]) Item[T] {
	if !typ.Valid() {
		typ.Drop(value)
		return Item[T]{}
	}
	traits := typ.Traits()
	return NewItemWithTraits(value, traits.Fork, traits.Drop)
}

// NewItemWithTraits takes ownership of value under explicit traits. Transports
// that rebuild a cell from stored ownership use it; components use NewItem.
func NewItemWithTraits[T any](value T, fork func(T) T, drop func(T)) Item[T] {
	return Item[T]{value: value, fork: fork, drop: drop, valid: true}
}

// Set takes ownership of value, releasing anything the cell still held. A
// stage that emits many items keeps one cell and reuses it through Set, which
// costs no allocation because the cell never escapes per item. Building a
// fresh cell per item is equally correct and allocates once.
func (i *Item[T]) Set(value T, typ schema.Type[T]) {
	if i == nil || !typ.Valid() {
		typ.Drop(value)
		if i != nil {
			i.Drop()
		}
		return
	}
	traits := typ.Traits()
	i.Drop()
	i.value, i.fork, i.drop, i.valid = value, traits.Fork, traits.Drop, true
}

// Valid reports whether the cell still holds an unreleased value.
func (i *Item[T]) Valid() bool { return i != nil && i.valid }

// Value borrows the held value for the duration of the current call. It stays
// valid only until the cell is consumed.
func (i *Item[T]) Value() T {
	if i == nil {
		var zero T
		return zero
	}
	return i.value
}

// Drop releases the held value. It is safe to defer immediately after
// receiving or creating a cell: once something else has consumed the cell,
// Drop does nothing.
func (i *Item[T]) Drop() {
	if i == nil || !i.valid {
		return
	}
	value, drop := i.value, i.drop
	i.clear()
	if drop != nil {
		drop(value)
	}
}

// Move transfers ownership from source into i, leaving source empty. Anything
// i still held is released first.
func (i *Item[T]) Move(source *Item[T]) {
	if i == nil || source == nil || i == source {
		return
	}
	i.Drop()
	if !source.valid {
		return
	}
	i.value, i.fork, i.drop, i.valid = source.value, source.fork, source.drop, true
	source.clear()
}

// Fork places an independent owner of the same logical value into target. The
// receiver keeps its own ownership, so only genuine fan-out retains.
func (i *Item[T]) Fork(target *Item[T]) bool {
	if i == nil || target == nil || !i.valid {
		return false
	}
	value := i.value
	if i.fork != nil {
		value = i.fork(value)
	}
	target.Drop()
	target.value, target.fork, target.drop, target.valid = value, i.fork, i.drop, true
	return true
}

// Detach empties the cell and returns the held value without releasing it, so
// the caller becomes responsible for it. A transport that must hold an item
// outside a call stack uses it together with SetWithTraits; components move
// cells instead.
//
// It returns no token, because a token that can be copied can be released
// twice, which is exactly what a cell exists to prevent.
func (i *Item[T]) Detach() (T, bool) {
	if i == nil || !i.valid {
		var zero T
		return zero, false
	}
	value := i.value
	i.clear()
	return value, true
}

// SetWithTraits takes ownership of value under explicit traits, releasing
// anything the cell still held. It is the counterpart of Detach for a
// transport that carries traits separately from the values it stores.
func (i *Item[T]) SetWithTraits(value T, fork func(T) T, drop func(T)) {
	if i == nil {
		if drop != nil {
			drop(value)
		}
		return
	}
	i.Drop()
	i.value, i.fork, i.drop, i.valid = value, fork, drop, true
}

func (i *Item[T]) clear() {
	var zero T
	i.value, i.fork, i.drop, i.valid = zero, nil, nil, false
}

// Transfer moves ownership from source into target, rewrapping the payload
// for a different item type. It is the linear hand-off: source is emptied
// without releasing, so the payload is never retained and no second owner
// exists at any point.
//
// build must carry source's payload into its result, which is what makes the
// unreleased source correct. Detaching the payload is therefore the last thing
// build should do, because a failure afterwards strands it. Use Fork instead
// when both items must stay alive.
func Transfer[I, O any](source *Item[I], target *Item[O], typ schema.Type[O], build func(I) (O, error)) error {
	if source == nil || target == nil || !source.valid || build == nil {
		return ErrTransfer
	}
	value, drop := source.value, source.drop
	source.clear()
	// The source cell is empty from here, so its owner cannot release the
	// value any more. Releasing on every path out of build -- returned error
	// or panic -- is what keeps that emptying safe.
	handed := false
	defer func() {
		if !handed && drop != nil {
			drop(value)
		}
	}()
	result, err := build(value)
	if err != nil {
		return err
	}
	handed = true
	target.Set(result, typ)
	return nil
}
