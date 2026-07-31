//go:build goexperiment.simd && amd64

package mp3

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/godexture/godec/plugins/codec-mp3/internal/mp3/layer3"
	"github.com/godexture/godec/sdk/dsp"
)

// dctType2 is only ever called with bandCount == SamplesPerSubBandLayer3 (18)
// or SamplesPerSubBandLayer12 (12): the algorithm addresses up to column
// bandCount-1 across all 32 conceptual rows of a single channel's
// layer3.SamplesPerGranule (576 = 32*18) buffer, so bandCount above 18
// would run off the end of that buffer regardless of scalar or SIMD.
func TestDCTType2SIMD(t *testing.T) {
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, bandCount := range []int{0, 1, 2, 3, 4, 7, 8, 9, 12, 15, 16, 17, 18} {
		input := make([]float32, layer3.SamplesPerGranule)
		for i := range input {
			input[i] = rand.Float32()*2 - 1
		}
		want := slices.Clone(input)
		got := slices.Clone(input)
		dctType2Scalar(want, bandCount)
		dctType2SIMD(got, bandCount)
		if !slices.Equal(got, want) {
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("bandCount %d: sample %d: got %v, want %v", bandCount, i, got[i], want[i])
				}
			}
		}
	}
}
