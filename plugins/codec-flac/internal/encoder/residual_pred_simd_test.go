//go:build goexperiment.simd && amd64

package encoder

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/godexture/godec/sdk/dsp"
)

func TestLPCResidualSIMD(t *testing.T) {
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, bitsPerSample := range []int{8, 16, 24, 31, 32} {
		for order := 1; order <= 32; order++ {
			coefficients := make([]int64, order)
			for i := range coefficients {
				coefficients[i] = int64(rand.Int32N(1<<15) - 1<<14)
			}
			for _, shift := range []int{0, 1, 7, 15} {
				for remainder := 0; remainder < 8; remainder++ {
					samples := randomResidualSamples(order+32+remainder, bitsPerSample)
					want := lpcResidualScalar(samples, order, coefficients, shift)
					got := lpcResidualSIMD(samples, order, coefficients, shift)
					if !slices.Equal(got, want) {
						t.Fatalf("bps %d order %d shift %d remainder %d", bitsPerSample, order, shift, remainder)
					}
					releaseResidualBuffer(want)
					releaseResidualBuffer(got)
				}
			}
		}
	}
}

func TestFixedResidualSIMD(t *testing.T) {
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for order := 0; order <= 4; order++ {
		for remainder := 0; remainder < 8; remainder++ {
			samples := randomResidualSamples(order+32+remainder, 32)
			want := fixedResidualScalar(samples, order)
			got := fixedResidualSIMD(samples, order)
			if !slices.Equal(got, want) {
				t.Fatalf("order %d remainder %d", order, remainder)
			}
			releaseResidualBuffer(want)
			releaseResidualBuffer(got)
		}
	}
}

func randomResidualSamples(length, bitsPerSample int) []int64 {
	samples := make([]int64, length)
	if bitsPerSample == 32 {
		for i := range samples {
			samples[i] = int64(rand.Int32())
		}
		return samples
	}
	limit := int64(1) << uint(bitsPerSample-1)
	for i := range samples {
		samples[i] = rand.Int64N(2*limit) - limit
	}
	return samples
}
