//go:build goexperiment.simd && amd64

package encoder

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestWindowSamplesSIMD(t *testing.T) {
	requireAVX2(t)
	for _, length := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 31, 32, 33, 4096, 4099} {
		samples := make([]int64, length)
		window := make([]float64, length)
		for i := range samples {
			samples[i] = int64(rand.Int32())
			window[i] = float64((i%17)+1) / 17
		}
		for _, currentWindow := range [][]float64{nil, window} {
			want := make([]float64, length)
			got := make([]float64, length)
			windowSamplesScalar(samples, currentWindow, want)
			windowSamplesSIMD(samples, currentWindow, got)
			if !slices.Equal(got, want) {
				t.Fatalf("length %d window=%v: got %v, want %v", length, currentWindow != nil, got, want)
			}
		}
	}
}

func TestWindowSamplesDispatches33BitToScalar(t *testing.T) {
	requireAVX2(t)
	samples := []int64{1 << 32, -(1 << 32), 1, -1}
	want := make([]float64, len(samples))
	got := make([]float64, len(samples))
	windowSamplesScalar(samples, nil, want)
	windowSamples(samples, nil, got, 33)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
