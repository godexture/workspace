//go:build goexperiment.simd && amd64

package layer3

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/godexture/sdk/dsp"
)

func TestAntialiasSIMD(t *testing.T) {
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for bandCount := 1; bandCount <= 32; bandCount++ {
		input := make([]float32, (bandCount+1)*SamplesPerSubBand+8)
		for i := range input {
			input[i] = rand.Float32()*2 - 1
		}
		want := slices.Clone(input)
		got := slices.Clone(input)
		antialiasScalar(want, bandCount)
		antialiasSIMD(got, bandCount)
		if !slices.Equal(got, want) {
			t.Fatalf("band count %d", bandCount)
		}
	}
}
