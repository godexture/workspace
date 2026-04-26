// pkg/pool/buffer.go
package pool

import (
	"math/bits"
	"sync"
)

const (
	minShift = 6
	maxShift = 20
)

var pools [maxShift - minShift + 1]*sync.Pool

func init() {
	for i := range pools {
		size := 1 << (i + minShift)
		pools[i] = &sync.Pool{
			New: func() any {
				b := make([]byte, size)
				return &b
			},
		}
	}
}

func Get(size int) *[]byte {
	if size <= 1<<minShift {
		return pools[0].Get().(*[]byte)
	}
	if size > 1<<maxShift {
		b := make([]byte, size)
		return &b
	}

	idx := bits.Len(uint(size-1)) - minShift
	b := pools[idx].Get().(*[]byte)
	return b
}

func Put(b *[]byte) {
	cap := cap(*b)
	if cap < 1<<minShift || cap > 1<<maxShift {
		return
	}

	idx := capToIndex(cap)
	if idx >= 0 && idx < len(pools) {
		*b = (*b)[:0]
		pools[idx].Put(b)
	}
}

func capToIndex(cap int) int {
	return bits.Len(uint(cap)-1) - minShift
}
