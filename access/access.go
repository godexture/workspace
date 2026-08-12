// Package access defines narrow byte capabilities and explicit ownership.
package access

import (
	"context"
	"io"
	"sync"
)

// Sequential and Random are the narrow read views a component receives
// instead of a struct whose unavailable operations are nil. A component
// declares what it needs through Requirements and is handed only that.
type Sequential interface {
	Read(context.Context, []byte) (int, error)
}

type Random interface {
	ReadAt(context.Context, []byte, int64) (int, error)
}

// Sizer reports the immutable byte length promised by StableSize.
type Sizer interface {
	Size(context.Context) (int64, error)
}

// Appender and Patcher are the narrow write views corresponding to
// SequentialWrite and RandomWrite.
type Appender interface {
	Write(context.Context, []byte) (int, error)
}

type Patcher interface {
	WriteAt(context.Context, []byte, int64) (int, error)
}

type Ownership uint8

const (
	Owned Ownership = iota + 1
	Borrowed
)

func (o Ownership) Valid() bool { return o == Owned || o == Borrowed }

// Resource describes a direct handle and whether Host closes it. No provider,
// filesystem, network, or device implementation is part of this package.
type Resource[T any] struct {
	value     T
	ownership Ownership
	state     *resourceState
}

// Direct is the non-owning view handed to one explicitly selected adaptor.
// It exposes the typed handle but no Close operation; Host remains the sole
// owner of Resource cleanup.
type Direct[T any] struct {
	value     T
	ownership Ownership
}

func (d Direct[T]) Value() T             { return d.value }
func (d Direct[T]) Ownership() Ownership { return d.ownership }
func (d Direct[T]) Valid() bool          { return d.ownership.Valid() }

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
func (r Resource[T]) Direct() Direct[T]    { return Direct[T]{value: r.value, ownership: r.ownership} }

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

func closeFunc[T any](value T) func() error {
	closer, ok := any(value).(io.Closer)
	if !ok {
		return nil
	}
	return closer.Close
}
