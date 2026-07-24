//go:build goexperiment.simd && amd64

package mp3

import "testing"

func BenchmarkDCTType2Compare(b *testing.B) {
	granule := make([]float32, SamplesPerSubBandLayer3*SubBandCount)
	for i := range granule {
		granule[i] = float32(i%31-15) / 16
	}
	b.Run("scalar", func(b *testing.B) {
		for b.Loop() {
			dctType2Scalar(granule, SamplesPerSubBandLayer3)
		}
	})
	b.Run("simd", func(b *testing.B) {
		for b.Loop() {
			dctType2SIMD(granule, SamplesPerSubBandLayer3)
		}
	})
}
