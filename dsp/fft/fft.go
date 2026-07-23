// Package fft provides fixed-size, power-of-two, real-valued-friendly FFT
// primitives for streaming DSP use (in particular partitioned convolution).
// Transform sizes are always chosen by the caller ahead of time, so the
// package only implements radix-2 Cooley-Tukey: arbitrary-length
// algorithms (Bluestein, mixed-radix) are out of scope by design.
package fft

import (
	"fmt"
	"math"
)

// Plan holds precomputed twiddle factors and a bit-reversal table for a
// fixed power-of-two transform size. Precomputation happens once in
// NewPlan; Forward and Inverse do no further allocation.
//
// A Plan is not safe for concurrent use. Callers that need concurrent
// transforms create one Plan per goroutine.
type Plan struct {
	n        int
	twiddles []complex64 // twiddles[k] = exp(-2*pi*i*k/n), k = 0..n/2-1
	bitrev   []int32
}

// NewPlan precomputes a transform plan for size n, which must be a power
// of two no smaller than 2.
func NewPlan(n int) (*Plan, error) {
	if n < 2 || n&(n-1) != 0 {
		return nil, fmt.Errorf("fft: size must be a power of two >= 2, got %d", n)
	}
	return &Plan{
		n:        n,
		twiddles: computeTwiddles(n),
		bitrev:   computeBitReversal(n),
	}, nil
}

// Size returns the transform length this plan was built for.
func (p *Plan) Size() int { return p.n }

// Clone creates an independently usable plan while sharing the immutable
// precomputed tables. It is useful when a caller needs concurrent transforms
// of the same size without recomputing twiddles or bit-reversal indices.
func (p *Plan) Clone() *Plan {
	return &Plan{n: p.n, twiddles: p.twiddles, bitrev: p.bitrev}
}

// Forward runs an in-place radix-2 decimation-in-time FFT. len(data) must
// equal p.Size().
func (p *Plan) Forward(data []complex64) error {
	if len(data) != p.n {
		return fmt.Errorf("fft: data length %d, want %d", len(data), p.n)
	}
	transformRadix2Scalar(p.twiddles, p.bitrev, data, false)
	return nil
}

// Inverse runs an in-place inverse FFT, including the 1/n scaling.
// len(data) must equal p.Size().
func (p *Plan) Inverse(data []complex64) error {
	if len(data) != p.n {
		return fmt.Errorf("fft: data length %d, want %d", len(data), p.n)
	}
	transformRadix2Scalar(p.twiddles, p.bitrev, data, true)
	return nil
}

// computeTwiddles precomputes exp(-2*pi*i*k/n) for k = 0..n/2-1. A single
// table of this size serves every butterfly stage: the twiddle used at
// stage length L for butterfly index j is twiddles[j*(n/L)].
func computeTwiddles(n int) []complex64 {
	twiddles := make([]complex64, n/2)
	for k := range twiddles {
		angle := -2 * math.Pi * float64(k) / float64(n)
		sin, cos := math.Sincos(angle)
		twiddles[k] = complex(float32(cos), float32(sin))
	}
	return twiddles
}

// computeBitReversal precomputes, for each index i, the index obtained by
// reversing the low log2(n) bits of i.
func computeBitReversal(n int) []int32 {
	shift := 0
	for 1<<shift < n {
		shift++
	}
	rev := make([]int32, n)
	for i := range rev {
		r := 0
		x := i
		for b := 0; b < shift; b++ {
			r = (r << 1) | (x & 1)
			x >>= 1
		}
		rev[i] = int32(r)
	}
	return rev
}
