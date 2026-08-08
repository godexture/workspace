// Package access defines narrow byte capabilities and explicit ownership.
package access

import (
	"context"
	"io"
	"sync"
)

// Sequential and Random are the narrow views a component receives instead of a
// struct whose unavailable operations are nil. A component declares what it
// needs through Requirements and is handed only that.
//
// Only these two exist today. The views for stable size, reopen, cancel, and
// the write side are added by the milestone that first hands one out, so their
// shape is decided against a real caller rather than guessed here.
type Sequential interface {
	Read(context.Context, []byte) (int, error)
}

type Random interface {
	ReadAt(context.Context, []byte, int64) (int, error)
}

type Ownership uint8

const (
	Owned Ownership = iota + 1
	Borrowed
	FactoryOwned
)

func (o Ownership) Valid() bool { return o >= Owned && o <= FactoryOwned }

// Resource describes a direct handle and whether Host closes it. No provider,
// filesystem, network, or device implementation is part of this package.
type Resource[T any] struct {
	value     T
	ownership Ownership
	state     *resourceState
}

type resourceState struct {
	once     sync.Once
	close    func() error
	closeErr error
}

func Own[T any](value T) Resource[T] {
	return Resource[T]{value: value, ownership: Owned, state: &resourceState{close: closeFunc(value)}}
}

func Borrow[T any](value T) Resource[T] {
	return Resource[T]{value: value, ownership: Borrowed}
}

func (r Resource[T]) Value() T             { return r.value }
func (r Resource[T]) Ownership() Ownership { return r.ownership }
func (r Resource[T]) Valid() bool          { return r.ownership.Valid() }

func (r *Resource[T]) Close() error {
	if r == nil || r.state == nil {
		return nil
	}
	r.state.once.Do(func() {
		if r.state.close != nil {
			r.state.closeErr = r.state.close()
		}
	})
	return r.state.closeErr
}

type SessionFactory[T any] struct {
	open func(context.Context) (T, error)
}

func Factory[T any](open func(context.Context) (T, error)) SessionFactory[T] {
	return SessionFactory[T]{open: open}
}

func (f SessionFactory[T]) Open(ctx context.Context) (Resource[T], error) {
	value, err := f.open(ctx)
	if err != nil {
		return Resource[T]{}, err
	}
	resource := Own(value)
	resource.ownership = FactoryOwned
	return resource, nil
}

func closeFunc[T any](value T) func() error {
	closer, ok := any(value).(io.Closer)
	if !ok {
		return nil
	}
	return closer.Close
}
