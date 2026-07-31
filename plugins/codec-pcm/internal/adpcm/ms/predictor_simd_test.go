//go:build goexperiment.simd && amd64

package msadpcm

import (
	"math/rand/v2"
	"testing"

	"github.com/godexture/godec/plugins/format-wav/params"
	"github.com/godexture/godec/sdk/dsp"
)

func TestFindBestPredictorSIMD(t *testing.T) {
	if !dsp.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	coefficients := predictorTestCoefficients()
	for _, samplesPerBlock := range []int{2, 3, 17, 1010, 2034} {
		for _, channels := range []int{1, 2} {
			samples := make([]int16, samplesPerBlock*channels)
			for i := range samples {
				samples[i] = int16(rand.Uint32())
			}
			for offset := 0; offset < channels; offset++ {
				want := findBestPredictorScalar(samples, samplesPerBlock, channels, offset, coefficients)
				got := findBestPredictorSIMD(samples, samplesPerBlock, channels, offset, coefficients)
				if got != want {
					t.Fatalf("samples %d channels %d offset %d: got %d, want %d", samplesPerBlock, channels, offset, got, want)
				}
			}
		}
	}
	for _, samples := range [][]int16{
		make([]int16, 2034),
		alternatingPredictorSamples(2034),
		constantPredictorSamples(2034, 32767),
		constantPredictorSamples(2034, -32768),
	} {
		want := findBestPredictorScalar(samples, len(samples), 1, 0, coefficients)
		got := findBestPredictorSIMD(samples, len(samples), 1, 0, coefficients)
		if got != want {
			t.Fatalf("adversarial input: got %d, want %d", got, want)
		}
	}
}

func predictorTestCoefficients() []params.Coefficient {
	coefficients := make([]params.Coefficient, len(coeffs))
	for i, coefficient := range coeffs {
		coefficients[i] = params.Coefficient{Coeff1: int16(coefficient[0]), Coeff2: int16(coefficient[1])}
	}
	return coefficients
}

func alternatingPredictorSamples(length int) []int16 {
	samples := make([]int16, length)
	for i := range samples {
		if i%2 == 0 {
			samples[i] = -32768
		} else {
			samples[i] = 32767
		}
	}
	return samples
}

func constantPredictorSamples(length int, value int16) []int16 {
	samples := make([]int16, length)
	for i := range samples {
		samples[i] = value
	}
	return samples
}
