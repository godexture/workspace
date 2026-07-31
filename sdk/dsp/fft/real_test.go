package fft

import (
	"fmt"
	"math"
	"math/cmplx"
	"testing"
)

var realTestSizes = []int{4, 8, 16, 64, 256}

func naiveRealDFT(x []float64) []complex128 {
	n := len(x)
	bins := n/2 + 1
	y := make([]complex128, bins)
	for k := range y {
		var sum complex128
		for t, xt := range x {
			angle := -2 * math.Pi * float64(k) * float64(t) / float64(n)
			sum += complex(xt, 0) * cmplx.Exp(complex(0, angle))
		}
		y[k] = sum
	}
	return y
}

func TestRealPlanForwardMatchesNaiveDFT(t *testing.T) {
	t.Parallel()
	for _, n := range realTestSizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			t.Parallel()
			plan, err := NewRealPlan(n)
			if err != nil {
				t.Fatal(err)
			}
			input := randomFloat32(n, int64(n))
			want := naiveRealDFT(toFloat64(input))

			spectrum := make([]complex64, plan.Bins())
			if err := plan.Forward(spectrum, input); err != nil {
				t.Fatal(err)
			}
			assertCloseComplex(t, toComplex128(spectrum), want, 1e-2)
		})
	}
}

func TestRealPlanForwardMatchesZeroPaddedComplexPlan(t *testing.T) {
	t.Parallel()
	for _, n := range realTestSizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			t.Parallel()
			realPlan, err := NewRealPlan(n)
			if err != nil {
				t.Fatal(err)
			}
			complexPlan, err := NewPlan(n)
			if err != nil {
				t.Fatal(err)
			}
			input := randomFloat32(n, int64(n)+1)

			spectrum := make([]complex64, realPlan.Bins())
			if err := realPlan.Forward(spectrum, input); err != nil {
				t.Fatal(err)
			}

			padded := make([]complex64, n)
			for i, v := range input {
				padded[i] = complex(v, 0)
			}
			if err := complexPlan.Forward(padded); err != nil {
				t.Fatal(err)
			}
			assertCloseComplex(t, toComplex128(spectrum), toComplex128(padded[:realPlan.Bins()]), 1e-3)
		})
	}
}

func TestRealPlanRoundTrip(t *testing.T) {
	t.Parallel()
	for _, n := range realTestSizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			t.Parallel()
			plan, err := NewRealPlan(n)
			if err != nil {
				t.Fatal(err)
			}
			input := randomFloat32(n, int64(n)+2)

			spectrum := make([]complex64, plan.Bins())
			if err := plan.Forward(spectrum, input); err != nil {
				t.Fatal(err)
			}
			output := make([]float32, n)
			if err := plan.Inverse(output, spectrum); err != nil {
				t.Fatal(err)
			}
			assertCloseFloat(t, toFloat64(output), toFloat64(input), 1e-3)
		})
	}
}

// TestRealPlanDCAndNyquist targets the edge case in Inverse where the DC
// (k=0) and Nyquist (k=n/2) bins must be combined explicitly: a signal
// that is purely DC exercises spectrum[0], and a signal that alternates
// sign every sample (the highest representable frequency) exercises
// spectrum[n/2].
func TestRealPlanDCAndNyquist(t *testing.T) {
	t.Parallel()
	const n = 16
	plan, err := NewRealPlan(n)
	if err != nil {
		t.Fatal(err)
	}
	bins := plan.Bins()

	t.Run("dc", func(t *testing.T) {
		input := make([]float32, n)
		for i := range input {
			input[i] = 1
		}
		spectrum := make([]complex64, bins)
		if err := plan.Forward(spectrum, input); err != nil {
			t.Fatal(err)
		}
		want := make([]complex128, bins)
		want[0] = complex(float64(n), 0)
		assertCloseComplex(t, toComplex128(spectrum), want, 1e-4)

		output := make([]float32, n)
		if err := plan.Inverse(output, spectrum); err != nil {
			t.Fatal(err)
		}
		assertCloseFloat(t, toFloat64(output), toFloat64(input), 1e-4)
	})

	t.Run("nyquist", func(t *testing.T) {
		input := make([]float32, n)
		for i := range input {
			if i%2 == 0 {
				input[i] = 1
			} else {
				input[i] = -1
			}
		}
		spectrum := make([]complex64, bins)
		if err := plan.Forward(spectrum, input); err != nil {
			t.Fatal(err)
		}
		want := make([]complex128, bins)
		want[bins-1] = complex(float64(n), 0)
		assertCloseComplex(t, toComplex128(spectrum), want, 1e-4)

		output := make([]float32, n)
		if err := plan.Inverse(output, spectrum); err != nil {
			t.Fatal(err)
		}
		assertCloseFloat(t, toFloat64(output), toFloat64(input), 1e-4)
	})
}

func TestNewRealPlanRejectsInvalidSize(t *testing.T) {
	t.Parallel()
	for _, n := range []int{-1, 0, 1, 2, 3, 6, 100} {
		if _, err := NewRealPlan(n); err == nil {
			t.Fatalf("NewRealPlan(%d) succeeded, want error", n)
		}
	}
}

func TestRealPlanRejectsWrongLength(t *testing.T) {
	t.Parallel()
	plan, err := NewRealPlan(16)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Forward(make([]complex64, plan.Bins()), make([]float32, 8)); err == nil {
		t.Fatal("Forward with wrong input length succeeded, want error")
	}
	if err := plan.Forward(make([]complex64, 3), make([]float32, 16)); err == nil {
		t.Fatal("Forward with wrong spectrum length succeeded, want error")
	}
	if err := plan.Inverse(make([]float32, 8), make([]complex64, plan.Bins())); err == nil {
		t.Fatal("Inverse with wrong output length succeeded, want error")
	}
	if err := plan.Inverse(make([]float32, 16), make([]complex64, 3)); err == nil {
		t.Fatal("Inverse with wrong spectrum length succeeded, want error")
	}
}
