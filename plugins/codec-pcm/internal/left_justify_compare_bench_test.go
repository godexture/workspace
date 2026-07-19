//go:build goexperiment.simd && amd64

package internal

import "testing"

func BenchmarkLeftJustifyCompare(b *testing.B) {
	source := make([]byte, 4096*4)
	destination := make([]byte, len(source))
	b.Run("s16-scalar", func(b *testing.B) {
		for b.Loop() {
			leftJustifyS16Scalar(destination, source, 4)
		}
	})
	b.Run("s16-simd", func(b *testing.B) {
		for b.Loop() {
			leftJustifyS16SIMD(destination, source, 4)
		}
	})
	b.Run("s32-scalar", func(b *testing.B) {
		for b.Loop() {
			leftJustifyS32Scalar(destination, source, 8)
		}
	})
	b.Run("s32-simd", func(b *testing.B) {
		for b.Loop() {
			leftJustifyS32SIMD(destination, source, 8)
		}
	})
}
