//go:build goexperiment.simd && amd64

package dsp

import "testing"

func BenchmarkConvertF32ToS16Compare(b *testing.B) {
	source := make([]float32, 4096)
	for i := range source {
		source[i] = float32((i*7919)%131072-65536) / 32768
	}
	destination := make([]int16, len(source))
	b.Run("scalar", func(b *testing.B) {
		for b.Loop() {
			convertF32ToS16Scalar(destination, source)
		}
	})
	b.Run("simd", func(b *testing.B) {
		for b.Loop() {
			convertF32ToS16SIMD(destination, source)
		}
	})
}
