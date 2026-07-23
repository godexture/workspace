package fft

import (
	"fmt"
	"math"
)

// RealPlan computes the FFT of n real samples using the standard
// half-length packing trick: pack the n real samples into an n/2-point
// complex sequence, run a complex Plan of that size, then unpack into the
// n/2+1 unique complex bins of the real spectrum (DC..Nyquist). This halves
// the work of running n real samples through a size-n complex transform
// with a zeroed imaginary part.
//
// A RealPlan is not safe for concurrent use, for the same reason as Plan.
type RealPlan struct {
	inner   *Plan
	n       int
	m       int
	unpack  []complex64 // unpack[k] = exp(-2*pi*i*k/n), k = 0..m
	scratch []complex64 // persistent scratch of length m, owned by the plan
}

// NewRealPlan precomputes a transform plan for n real samples, which must
// be a power of two no smaller than 4.
func NewRealPlan(n int) (*RealPlan, error) {
	if n < 4 || n&(n-1) != 0 {
		return nil, fmt.Errorf("fft: real size must be a power of two >= 4, got %d", n)
	}
	m := n / 2
	inner, err := NewPlan(m)
	if err != nil {
		return nil, err
	}
	return &RealPlan{
		inner:   inner,
		n:       n,
		m:       m,
		unpack:  computeUnpackTwiddles(n, m),
		scratch: make([]complex64, m),
	}, nil
}

// Size returns the real sample count this plan was built for.
func (p *RealPlan) Size() int { return p.n }

// Clone creates an independently usable real plan while sharing immutable
// transform tables. Its scratch workspace is private to the clone.
func (p *RealPlan) Clone() *RealPlan {
	return &RealPlan{
		inner:   p.inner.Clone(),
		n:       p.n,
		m:       p.m,
		unpack:  p.unpack,
		scratch: make([]complex64, p.m),
	}
}

// Bins returns the number of unique complex bins (DC..Nyquist) produced by
// Forward and consumed by Inverse: n/2 + 1.
func (p *RealPlan) Bins() int { return p.m + 1 }

// Forward computes the FFT of n real samples and writes the n/2+1 unique
// complex bins (DC..Nyquist) into spectrum. len(input) must equal p.Size();
// len(spectrum) must equal p.Bins().
func (p *RealPlan) Forward(spectrum []complex64, input []float32) error {
	if len(input) != p.n {
		return fmt.Errorf("fft: real input length %d, want %d", len(input), p.n)
	}
	if len(spectrum) != p.m+1 {
		return fmt.Errorf("fft: spectrum length %d, want %d", len(spectrum), p.m+1)
	}

	for i := range p.scratch {
		p.scratch[i] = complex(input[2*i], input[2*i+1])
	}
	if err := p.inner.Forward(p.scratch); err != nil {
		return err
	}

	// Z = FFT(pack(input)) is periodic with period m. Every bin, including
	// the k=0 (DC) and k=m (Nyquist) edges, is recovered by the same
	// formula pairing Z[k] with Z[(m-k) mod m]; no special-casing is
	// needed here (unlike Inverse, see below).
	for k := 0; k <= p.m; k++ {
		zk := p.scratch[k%p.m]
		zkc := p.scratch[(p.m-k)%p.m]
		fe := (zk + conjugate(zkc)) * complex(0.5, 0)
		fo := (zk - conjugate(zkc)) * complex(0, -0.5)
		spectrum[k] = fe + p.unpack[k]*fo
	}
	return nil
}

// Inverse is the exact inverse of Forward. len(spectrum) must equal
// p.Bins(); len(output) must equal p.Size().
func (p *RealPlan) Inverse(output []float32, spectrum []complex64) error {
	if len(spectrum) != p.m+1 {
		return fmt.Errorf("fft: spectrum length %d, want %d", len(spectrum), p.m+1)
	}
	if len(output) != p.n {
		return fmt.Errorf("fft: real output length %d, want %d", len(output), p.n)
	}

	// k=0 needs special handling: unlike every interior k, its natural
	// pairing index (m-0) mod m collapses back to 0 itself, which would
	// silently discard the Nyquist bin (spectrum[m]) instead of pairing
	// with it. DC and Nyquist are two independent real numbers that both
	// derive from the single complex Z[0]=Fe[0]+i*Fo[0], so they must be
	// combined explicitly here.
	dc, nyquist := real(spectrum[0]), real(spectrum[p.m])
	p.scratch[0] = complex((dc+nyquist)/2, (dc-nyquist)/2)

	for k := 1; k < p.m; k++ {
		xk := spectrum[k]
		xkc := spectrum[p.m-k]
		fe := (xk + conjugate(xkc)) * complex(0.5, 0)
		fo := (xk - conjugate(xkc)) * conjugate(p.unpack[k]) * complex(0.5, 0)
		p.scratch[k] = fe + complex(0, 1)*fo
	}

	if err := p.inner.Inverse(p.scratch); err != nil {
		return err
	}
	for i, z := range p.scratch {
		output[2*i] = real(z)
		output[2*i+1] = imag(z)
	}
	return nil
}

// computeUnpackTwiddles precomputes exp(-2*pi*i*k/n) for k = 0..m. This is
// a different (and differently sized) table from Plan's internal twiddles,
// which are indexed relative to the m-point inner transform rather than
// the n-point real spectrum.
func computeUnpackTwiddles(n, m int) []complex64 {
	twiddles := make([]complex64, m+1)
	for k := range twiddles {
		angle := -2 * math.Pi * float64(k) / float64(n)
		sin, cos := math.Sincos(angle)
		twiddles[k] = complex(float32(cos), float32(sin))
	}
	return twiddles
}

func conjugate(z complex64) complex64 { return complex(real(z), -imag(z)) }
