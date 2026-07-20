package encoder

import (
	"strconv"
	"testing"
	"time"
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

func BenchmarkLPCResidualScalarPaired(b *testing.B) {
	samples := benchmarkBlock(4096)[0]
	for _, order := range []int{4, 8, 12, 32} {
		coefficients := make([]int64, order)
		for i := range coefficients {
			coefficients[i] = int64(i%7 - 3)
		}
		b.Run("order"+strconv.Itoa(order), func(b *testing.B) {
			var currentDuration, serialDuration time.Duration
			var iterations int64
			for b.Loop() {
				runCurrent := func() {
					start := time.Now()
					result := lpcResidualScalar(samples, order, coefficients, 7)
					currentDuration += time.Since(start)
					releaseResidualBuffer(result)
				}
				runSerial := func() {
					start := time.Now()
					result := lpcResidualScalarSerial(samples, order, coefficients, 7)
					serialDuration += time.Since(start)
					releaseResidualBuffer(result)
				}
				if iterations&1 == 0 {
					runCurrent()
					runSerial()
				} else {
					runSerial()
					runCurrent()
				}
				iterations++
			}
			if iterations > 0 {
				b.ReportMetric(float64(currentDuration.Nanoseconds())/float64(iterations), "current-ns/op")
				b.ReportMetric(float64(serialDuration.Nanoseconds())/float64(iterations), "serial-ns/op")
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
