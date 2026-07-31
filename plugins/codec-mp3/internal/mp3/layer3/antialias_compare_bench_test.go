//go:build goexperiment.simd && amd64

package layer3

import "testing"

func BenchmarkAntialiasCompare(b *testing.B) {
	granule := make([]float32, (SubBandCount+1)*SamplesPerSubBand)
	b.Run("scalar", func(b *testing.B) {
		for b.Loop() {
			antialiasScalar(granule, SubBandCount)
		}
	})
	b.Run("simd", func(b *testing.B) {
		for b.Loop() {
			antialiasSIMD(granule, SubBandCount)
		}
	})
}
