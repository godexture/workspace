package pool

import "sync"

// Typed wraps sync.Pool with a type parameter, removing the
// type-assert-after-Get boilerplate every call site otherwise repeats.
// The zero value is usable, matching sync.Pool itself; set New to construct
// a fresh value on a pool miss.
type Typed[T any] struct {
	pool sync.Pool
}

// Init sets the pool's New function to return a fresh value of type T.
func (p *Typed[T]) Init(newFunc func() T) {
	p.pool.New = func() any { return newFunc() }
}

// Get returns a pooled value, or New() on a miss. If New is nil, a miss
// returns the zero value of T, mirroring sync.Pool.Get's own contract for an
// unset New.
func (p *Typed[T]) Get() T {
	if v := p.pool.Get(); v != nil {
		return v.(T)
	}
	var zero T
	return zero
}

// Put returns v to the pool for reuse.
func (p *Typed[T]) Put(v T) {
	p.pool.Put(v)
}
