//go:build goexperiment.simd && amd64

package decoder

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/godexture/sdk/dsp"
)

func TestRestoreLPCSIMD(t *testing.T) {
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, bitsPerSample := range []int{8, 16, 17, 24, 25, 32} {
		for order := 1; order <= 32; order++ {
			coefficients := make([]int64, order)
			for i := range coefficients {
				coefficients[i] = int64(i%5 - 2)
			}
			decoded := make([]int64, order+67)
			for i := range decoded {
				decoded[i] = rand.Int64N(201) - 100
			}
			encoded := encodeLPCResiduals(decoded, coefficients, order, 7)
			want := slices.Clone(encoded)
			got := slices.Clone(encoded)
			restoreLPCScalarUnchecked(want, coefficients, order, 7)
			restoreLPCSIMDUnchecked(got, coefficients, order, 7)
			if !slices.Equal(got, want) || !slices.Equal(got, decoded) {
				t.Fatalf("bits=%d order=%d: got %v, want %v", bitsPerSample, order, got, want)
			}
		}
	}
}

func encodeLPCResiduals(decoded, coefficients []int64, order, shift int) []int64 {
	encoded := slices.Clone(decoded)
	for i := order; i < len(decoded); i++ {
		var prediction int64
		for j, coefficient := range coefficients {
			prediction += coefficient * decoded[i-1-j]
		}
		encoded[i] = decoded[i] - (prediction >> shift)
	}
	return encoded
}
