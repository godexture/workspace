package encoder

import (
	"strconv"
	"testing"
)

func BenchmarkLPCResidual(b *testing.B) {
	samples := benchmarkBlock(4096)[0]
	for _, order := range []int{8, 32} {
		b.Run("order"+strconv.Itoa(order), func(b *testing.B) {
			coefficients := make([]int64, order)
			for i := range coefficients {
				coefficients[i] = int64(i%7 - 3)
			}
			b.ReportAllocs()
			for b.Loop() {
				result := lpcResidual(samples, order, coefficients, 7, 16)
				releaseResidualBuffer(result)
			}
		})
	}
}

func BenchmarkFixedResidual(b *testing.B) {
	samples := benchmarkBlock(4096)[0]
	b.ReportAllocs()
	for b.Loop() {
		result := fixedResidual(samples, 4)
		releaseResidualBuffer(result)
	}
}
