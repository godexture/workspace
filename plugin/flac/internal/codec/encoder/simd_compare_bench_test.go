//go:build goexperiment.simd && amd64

package encoder

import (
	"strconv"
	"testing"

	"github.com/godexture/godec/plugin/flac/internal/codec/config"
	"github.com/godexture/godec/sdk/dsp"
)

func BenchmarkAutocorrelateCompare(b *testing.B) {
	values := make([]float64, 4096)
	for i := range values {
		values[i] = float64((i*7919)%65536 - 32768)
	}
	for _, order := range []int{1, 2, 3, 4, 6, 8, 12, 16, 32} {
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

func BenchmarkRiceStatsCompare(b *testing.B) {
	for _, length := range []int{16, 32, 64, 128, 256, 512, 1024, 4096} {
		values := make([]uint64, length)
		residual := make([]int64, length)
		folded := make([]uint64, length)
		for i := range values {
			residual[i] = int64((i*7919)%65536 - 32768)
			values[i] = foldResidual(residual[i])
		}
		b.Run("sum-max-"+strconv.Itoa(length), func(b *testing.B) {
			b.Run("scalar", func(b *testing.B) {
				for b.Loop() {
					sumMaxUint64Scalar(values)
				}
			})
			b.Run("simd", func(b *testing.B) {
				for b.Loop() {
					sumMaxUint64SIMD(values)
				}
			})
		})
		b.Run("fold-"+strconv.Itoa(length), func(b *testing.B) {
			b.Run("scalar", func(b *testing.B) {
				for b.Loop() {
					foldResidualBatchScalar(residual, folded)
				}
			})
			b.Run("simd", func(b *testing.B) {
				for b.Loop() {
					foldResidualBatchSIMD(residual, folded)
				}
			})
		})
		b.Run("fold-sum-max-"+strconv.Itoa(length), func(b *testing.B) {
			b.Run("scalar", func(b *testing.B) {
				for b.Loop() {
					foldSumMaxScalar(residual)
				}
			})
			b.Run("simd", func(b *testing.B) {
				for b.Loop() {
					foldSumMaxSIMD(residual)
				}
			})
		})
	}
}

func BenchmarkWindowSamplesCompare(b *testing.B) {
	for _, length := range []int{4, 16, 64, 256, 4096} {
		samples := benchmarkBlock(length)[0]
		window := make([]float64, len(samples))
		values := make([]float64, len(samples))
		for i := range window {
			window[i] = float64((i%17)+1) / 17
		}
		b.Run(strconv.Itoa(length), func(b *testing.B) {
			for _, currentWindow := range [][]float64{nil, window} {
				name := "nil"
				if currentWindow != nil {
					name = "window"
				}
				b.Run(name, func(b *testing.B) {
					b.Run("scalar", func(b *testing.B) {
						for b.Loop() {
							windowSamplesScalar(samples, currentWindow, values)
						}
					})
					b.Run("simd", func(b *testing.B) {
						for b.Loop() {
							windowSamplesSIMD(samples, currentWindow, values)
						}
					})
				})
			}
		})
	}
}

func BenchmarkEncodeFrameSIMDCompare(b *testing.B) {
	hasAVX2, hasAVX2FMA := dsp.HasAVX2, dsp.HasAVX2FMA
	b.Cleanup(func() {
		dsp.HasAVX2 = hasAVX2
		dsp.HasAVX2FMA = hasAVX2FMA
	})
	run := func(b *testing.B, avx2, fma bool, block [][]int64, config config.EncoderConfig) {
		dsp.HasAVX2 = avx2
		dsp.HasAVX2FMA = fma
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := EncodeFrame(block, 44100, 16, uint64(i), config); err != nil {
				b.Fatal(err)
			}
		}
	}
	for _, preset := range []int{3, 5, 7} {
		config := config.GetPreset(preset)
		block := benchmarkBlock(config.BlockSize)
		b.Run("preset"+strconv.Itoa(preset), func(b *testing.B) {
			b.Run("scalar", func(b *testing.B) {
				run(b, false, false, block, config)
			})
			b.Run("simd", func(b *testing.B) {
				run(b, hasAVX2, hasAVX2FMA, block, config)
			})
		})
	}
}
