//go:build goexperiment.simd && amd64

package internal

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/godexture/sdk/dsp"
)

func TestLeftJustifySIMD(t *testing.T) {
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for length := 0; length <= 137; length++ {
		source := make([]byte, length)
		for i := range source {
			source[i] = byte(rand.Uint32())
		}
		for _, shift := range []uint{1, 4, 7, 15} {
			want16, got16 := make([]byte, length), make([]byte, length)
			leftJustifyS16Scalar(want16, source, shift)
			leftJustifyS16SIMD(got16, source, shift)
			if !slices.Equal(got16, want16) {
				t.Fatalf("S16 length %d shift %d", length, shift)
			}

			want32, got32 := make([]byte, length), make([]byte, length)
			leftJustifyS32Scalar(want32, source, shift)
			leftJustifyS32SIMD(got32, source, shift)
			if !slices.Equal(got32, want32) {
				t.Fatalf("S32 length %d shift %d", length, shift)
			}
		}
	}
}
