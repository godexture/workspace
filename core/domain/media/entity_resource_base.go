package media

import (
	"sync/atomic"
)

type ResourceBase struct {
	refCount atomic.Int32
	freeFunc func()
}

func (r *ResourceBase) Init(free func()) {
	r.refCount.Store(1)
	r.freeFunc = free
}

func (r *ResourceBase) Retain() {
	if r.refCount.Add(1) <= 1 {
		panic("retained after release")
	}
}

func (r *ResourceBase) Release() {
	if count := r.refCount.Add(-1); count == 0 {
		if r.freeFunc != nil {
			r.freeFunc()
		}
	} else if count < 0 {
		panic("double release")
	}
}
