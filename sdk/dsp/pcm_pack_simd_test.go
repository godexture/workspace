//go:build goexperiment.simd && amd64

package dsp

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestPackS32StereoSIMD(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 unavailable")
	}
	for _, blockSize := range []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 4096, 4099} {
		left := make([]int64, blockSize)
		right := make([]int64, blockSize)
		for i := range left {
			left[i] = int64(1000 + i)
			right[i] = int64(2000 + i)
		}
		if blockSize > 9 {
			for i := range left {
				left[i] = int64(rand.Int32())
				right[i] = int64(rand.Int32())
			}
		}
		assertPackS32Equal(t, left, right, false)
	}
}

func TestPackS32StereoSIMDMisaligned(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 unavailable")
	}
	assertPackS32Equal(t, []int64{1, 2, 3, 4}, []int64{5, 6, 7, 8}, true)
}

func TestPackS32DispatchesMonoToScalar(t *testing.T) {
	if !HasAVX2() {
		t.Skip("AVX2 unavailable")
	}
	samples := [][]int64{{1, 2, 3, 4}}
	want := make([]byte, 16)
	got := make([]byte, 16)
	packS32Scalar(want, samples, 4, 1)
	packS32(got, samples, 4, 1)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func assertPackS32Equal(t *testing.T, left, right []int64, misaligned bool) {
	t.Helper()
	offset := 0
	if misaligned {
		offset = 1
	}
	wantStorage := make([]byte, offset+len(left)*8)
	gotStorage := make([]byte, offset+len(left)*8)
	want := wantStorage[offset:]
	got := gotStorage[offset:]
	packS32Scalar(want, [][]int64{left, right}, len(left), 2)
	packS32StereoSIMD(got, left, right, len(left))
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
