//go:build goexperiment.simd && amd64

package msadpcm

import "testing"

func BenchmarkFindBestPredictorCompare(b *testing.B) {
	const samplesPerBlock = 1010
	samples := make([]int16, samplesPerBlock*2)
	for i := range samples {
		samples[i] = int16((i*7919)%65536 - 32768)
	}
	coefficients := predictorTestCoefficients()
	b.Run("scalar", func(b *testing.B) {
		for b.Loop() {
			_ = findBestPredictorScalar(samples, samplesPerBlock, 2, 0, coefficients)
		}
	})
	b.Run("simd", func(b *testing.B) {
		for b.Loop() {
			_ = findBestPredictorSIMD(samples, samplesPerBlock, 2, 0, coefficients)
		}
	})
}
