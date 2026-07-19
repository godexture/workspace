//go:build goexperiment.simd && amd64

package dsp

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestConvertF32ToS16SIMD(t *testing.T) {
	if !HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	boundaries := []float32{
		-4, -1, math.Nextafter32(-1, float32(math.Inf(-1))), math.Nextafter32(-1, 0),
		-0.5, -math.SmallestNonzeroFloat32, float32(math.Copysign(0, -1)), 0,
		math.SmallestNonzeroFloat32, 0.5, math.Nextafter32(1, 0),
		1, math.Nextafter32(1, float32(math.Inf(1))), 4,
	}
	for length := 0; length <= 67; length++ {
		source := make([]float32, length)
		for i := range source {
			if i < len(boundaries) {
				source[i] = boundaries[i]
			} else {
				source[i] = rand.Float32()*8 - 4
			}
		}
		want := make([]int16, length)
		got := make([]int16, length)
		convertF32ToS16Scalar(want, source)
		convertF32ToS16SIMD(got, source)
		if !slices.Equal(got, want) {
			t.Fatalf("length %d: got %v, want %v", length, got, want)
		}
	}
}
