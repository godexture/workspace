package fft

import (
	"fmt"
	"math"
	"math/cmplx"
	"testing"
)

var testSizes = []int{2, 4, 8, 16, 64, 256}

func naiveDFT(x []complex128, invert bool) []complex128 {
	n := len(x)
	sign := -1.0
	if invert {
		sign = 1.0
	}
	y := make([]complex128, n)
	for k := range y {
		var sum complex128
		for t, xt := range x {
			angle := sign * 2 * math.Pi * float64(k) * float64(t) / float64(n)
			sum += xt * cmplx.Exp(complex(0, angle))
		}
		if invert {
			sum /= complex(float64(n), 0)
		}
		y[k] = sum
	}
	return y
}

func TestPlanForwardMatchesNaiveDFT(t *testing.T) {
	t.Parallel()
	for _, n := range testSizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			t.Parallel()
			plan, err := NewPlan(n)
			if err != nil {
				t.Fatal(err)
			}
			input := randomComplex(n, int64(n))
			want := naiveDFT(toComplex128(input), false)

			data := append([]complex64(nil), input...)
			if err := plan.Forward(data); err != nil {
				t.Fatal(err)
			}
			assertCloseComplex(t, toComplex128(data), want, 1e-2)
		})
	}
}

func TestPlanInverseMatchesNaiveDFT(t *testing.T) {
	t.Parallel()
	for _, n := range testSizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			t.Parallel()
			plan, err := NewPlan(n)
			if err != nil {
				t.Fatal(err)
			}
			input := randomComplex(n, int64(n)+1)
			want := naiveDFT(toComplex128(input), true)

			data := append([]complex64(nil), input...)
			if err := plan.Inverse(data); err != nil {
				t.Fatal(err)
			}
			assertCloseComplex(t, toComplex128(data), want, 1e-2)
		})
	}
}

func TestPlanRoundTrip(t *testing.T) {
	t.Parallel()
	for _, n := range testSizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			t.Parallel()
			plan, err := NewPlan(n)
			if err != nil {
				t.Fatal(err)
			}
			input := randomComplex(n, int64(n)+2)

			data := append([]complex64(nil), input...)
			if err := plan.Forward(data); err != nil {
				t.Fatal(err)
			}
			if err := plan.Inverse(data); err != nil {
				t.Fatal(err)
			}
			assertCloseComplex(t, toComplex128(data), toComplex128(input), 1e-3)
		})
	}
}

func TestPlanImpulseIsFlatSpectrum(t *testing.T) {
	t.Parallel()
	const n = 16
	plan, err := NewPlan(n)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]complex64, n)
	data[0] = 1
	if err := plan.Forward(data); err != nil {
		t.Fatal(err)
	}
	want := make([]complex128, n)
	for i := range want {
		want[i] = 1
	}
	assertCloseComplex(t, toComplex128(data), want, 1e-5)
}

func TestPlanSingleFrequencyPeak(t *testing.T) {
	t.Parallel()
	const n = 32
	const bin = 5
	plan, err := NewPlan(n)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]complex64, n)
	for i := range data {
		angle := 2 * math.Pi * float64(bin) * float64(i) / float64(n)
		sin, cos := math.Sincos(angle)
		data[i] = complex(float32(cos), float32(sin))
	}
	if err := plan.Forward(data); err != nil {
		t.Fatal(err)
	}
	for i, v := range data {
		want := complex64(0)
		if i == bin {
			want = complex64(complex(float64(n), 0))
		}
		if diff := cmplxAbs(complex(float64(real(v)-real(want)), float64(imag(v)-imag(want)))); diff > 1e-3 {
			t.Fatalf("bin %d = %v, want %v", i, v, want)
		}
	}
}

func TestNewPlanRejectsInvalidSize(t *testing.T) {
	t.Parallel()
	for _, n := range []int{-1, 0, 1, 3, 5, 100} {
		if _, err := NewPlan(n); err == nil {
			t.Fatalf("NewPlan(%d) succeeded, want error", n)
		}
	}
}

func TestPlanRejectsWrongLength(t *testing.T) {
	t.Parallel()
	plan, err := NewPlan(8)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Forward(make([]complex64, 4)); err == nil {
		t.Fatal("Forward with wrong length succeeded, want error")
	}
	if err := plan.Inverse(make([]complex64, 16)); err == nil {
		t.Fatal("Inverse with wrong length succeeded, want error")
	}
}
