//go:build goexperiment.simd && amd64

package encoder

import (
	"strconv"
	"testing"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/sdk/dsp"
)

func BenchmarkAutocorrelateCompare(b *testing.B) {
	values := make([]float64, 4096)
	for i := range values {
		values[i] = float64((i*7919)%65536 - 32768)
	}
	for _, order := range []int{8, 16, 32} {
		b.Run("order"+strconv.Itoa(order), func(b *testing.B) {
			auto := make([]float64, order+1)
			b.Run("scalar", func(b *testing.B) {
				for b.Loop() {
					autocorrelateScalar(values, auto)
				}
			})
			b.Run("simd", func(b *testing.B) {
				for b.Loop() {
					autocorrelateSIMD(values, auto)
				}
			})
		})
	}
}

func BenchmarkResidualCompare(b *testing.B) {
	samples := benchmarkBlock(4096)[0]
	for _, order := range []int{8, 32} {
		b.Run("lpc-order"+strconv.Itoa(order), func(b *testing.B) {
			coefficients := make([]int64, order)
			for i := range coefficients {
				coefficients[i] = int64(i%7 - 3)
			}
			b.Run("scalar", func(b *testing.B) {
				for b.Loop() {
					result := lpcResidualScalar(samples, order, coefficients, 7)
					releaseResidualBuffer(result)
				}
			})
			b.Run("simd", func(b *testing.B) {
				for b.Loop() {
					result := lpcResidualSIMD(samples, order, coefficients, 7)
					releaseResidualBuffer(result)
				}
			})
		})
	}
	b.Run("fixed-order4", func(b *testing.B) {
		b.Run("scalar", func(b *testing.B) {
			for b.Loop() {
				result := fixedResidualScalar(samples, 4)
				releaseResidualBuffer(result)
			}
		})
		b.Run("simd", func(b *testing.B) {
			for b.Loop() {
				result := fixedResidualSIMD(samples, 4)
				releaseResidualBuffer(result)
			}
		})
	})
}

func BenchmarkEncodeFrameSIMDCompare(b *testing.B) {
	block := benchmarkBlock(flac.DefaultEncoderConfig.BlockSize)
	hasAVX2, hasAVX2FMA := dsp.HasAVX2, dsp.HasAVX2FMA
	b.Cleanup(func() {
		dsp.HasAVX2 = hasAVX2
		dsp.HasAVX2FMA = hasAVX2FMA
	})
	run := func(b *testing.B, avx2, fma bool) {
		dsp.HasAVX2 = avx2
		dsp.HasAVX2FMA = fma
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := EncodeFrame(block, 44100, 16, uint64(i), flac.DefaultEncoderConfig); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.Run("scalar", func(b *testing.B) {
		run(b, false, false)
	})
	b.Run("simd", func(b *testing.B) {
		run(b, hasAVX2, hasAVX2FMA)
	})
}
