package fft

import (
	"math"
	"math/rand"
	"testing"
)

func randomComplex(n int, seed int64) []complex64 {
	r := rand.New(rand.NewSource(seed))
	data := make([]complex64, n)
	for i := range data {
		data[i] = complex(float32(r.NormFloat64()), float32(r.NormFloat64()))
	}
	return data
}

func randomFloat32(n int, seed int64) []float32 {
	r := rand.New(rand.NewSource(seed))
	data := make([]float32, n)
	for i := range data {
		data[i] = float32(r.NormFloat64())
	}
	return data
}

func toComplex128(data []complex64) []complex128 {
	result := make([]complex128, len(data))
	for i, v := range data {
		result[i] = complex(float64(real(v)), float64(imag(v)))
	}
	return result
}

func toFloat64(data []float32) []float64 {
	result := make([]float64, len(data))
	for i, v := range data {
		result[i] = float64(v)
	}
	return result
}

func assertCloseComplex(t *testing.T, got, want []complex128, tol float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if diff := cmplxAbs(got[i] - want[i]); diff > tol {
			t.Fatalf("index %d = %v, want %v (diff %g, tol %g)", i, got[i], want[i], diff, tol)
		}
	}
}

func assertCloseFloat(t *testing.T, got, want []float64, tol float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if diff := math.Abs(got[i] - want[i]); diff > tol {
			t.Fatalf("index %d = %v, want %v (diff %g, tol %g)", i, got[i], want[i], diff, tol)
		}
	}
}

func cmplxAbs(z complex128) float64 {
	return math.Hypot(real(z), imag(z))
}
