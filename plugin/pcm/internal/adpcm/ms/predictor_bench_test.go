package msadpcm

import (
	"testing"

	"github.com/godexture/godec/plugin/wave/params"
)

func BenchmarkFindBestPredictor(b *testing.B) {
	const samplesPerBlock = 1010
	samples := make([]int16, samplesPerBlock*2)
	for i := range samples {
		samples[i] = int16((i*7919)%65536 - 32768)
	}
	coefficients := make([]params.Coefficient, len(coeffs))
	for i, coefficient := range coeffs {
		coefficients[i] = params.Coefficient{Coeff1: int16(coefficient[0]), Coeff2: int16(coefficient[1])}
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = findBestPredictor(samples, samplesPerBlock, 2, 0, coefficients)
	}
}
