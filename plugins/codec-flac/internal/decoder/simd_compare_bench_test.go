//go:build goexperiment.simd && amd64

package decoder

import (
	"strconv"
	"testing"
	"time"
)

func BenchmarkRestoreLPCValidation(b *testing.B) {
	const (
		length = 4096
		order  = 16
	)
	coefficients := make([]int64, order)
	for i := range coefficients {
		coefficients[i] = int64(i%5 - 2)
	}
	decoded := make([]int64, length)
	for i := range decoded {
		decoded[i] = int64((i*7919)%65536 - 32768)
	}
	encoded := encodeLPCResiduals(decoded, coefficients, order, 7)
	checkedSamples := make([]int64, length)
	uncheckedSamples := make([]int64, length)

	var checkedDuration, uncheckedDuration time.Duration
	var iterations int64
	b.ReportAllocs()
	for b.Loop() {
		if iterations&1 == 0 {
			copy(checkedSamples, encoded)
			start := time.Now()
			if err := restoreLPCScalar(checkedSamples, coefficients, order, 7, -1<<31, 1<<31-1, 32); err != nil {
				b.Fatal(err)
			}
			checkedDuration += time.Since(start)
			copy(uncheckedSamples, encoded)
			start = time.Now()
			restoreLPCScalarUnchecked(uncheckedSamples, coefficients, order, 7)
			uncheckedDuration += time.Since(start)
		} else {
			copy(uncheckedSamples, encoded)
			start := time.Now()
			restoreLPCScalarUnchecked(uncheckedSamples, coefficients, order, 7)
			uncheckedDuration += time.Since(start)
			copy(checkedSamples, encoded)
			start = time.Now()
			if err := restoreLPCScalar(checkedSamples, coefficients, order, 7, -1<<31, 1<<31-1, 32); err != nil {
				b.Fatal(err)
			}
			checkedDuration += time.Since(start)
		}
		iterations++
	}
	if iterations > 0 {
		b.ReportMetric(float64(checkedDuration.Nanoseconds())/float64(iterations), "checked-ns/op")
		b.ReportMetric(float64(uncheckedDuration.Nanoseconds())/float64(iterations), "unchecked-ns/op")
	}
}

func BenchmarkRestoreLPCPaired(b *testing.B) {
	const length = 4096
	for _, order := range []int{8, 12, 16, 20, 24, 28, 29, 30, 31, 32} {
		coefficients := make([]int64, order)
		for i := range coefficients {
			coefficients[i] = int64(i%5 - 2)
		}
		decoded := make([]int64, length)
		for i := range decoded {
			decoded[i] = int64((i*7919)%65536 - 32768)
		}
		encoded := encodeLPCResiduals(decoded, coefficients, order, 7)
		scalarSamples := make([]int64, length)
		simdSamples := make([]int64, length)

		b.Run("order-"+strconv.Itoa(order), func(b *testing.B) {
			var scalarDuration, simdDuration time.Duration
			var iterations int64
			b.ReportAllocs()
			for b.Loop() {
				if iterations&1 == 0 {
					copy(scalarSamples, encoded)
					start := time.Now()
					restoreLPCScalarUnchecked(scalarSamples, coefficients, order, 7)
					scalarDuration += time.Since(start)
					copy(simdSamples, encoded)
					start = time.Now()
					restoreLPCSIMDUnchecked(simdSamples, coefficients, order, 7)
					simdDuration += time.Since(start)
				} else {
					copy(simdSamples, encoded)
					start := time.Now()
					restoreLPCSIMDUnchecked(simdSamples, coefficients, order, 7)
					simdDuration += time.Since(start)
					copy(scalarSamples, encoded)
					start = time.Now()
					restoreLPCScalarUnchecked(scalarSamples, coefficients, order, 7)
					scalarDuration += time.Since(start)
				}
				iterations++
			}
			if iterations > 0 {
				b.ReportMetric(float64(scalarDuration.Nanoseconds())/float64(iterations), "scalar-ns/op")
				b.ReportMetric(float64(simdDuration.Nanoseconds())/float64(iterations), "simd-ns/op")
			}
		})
	}
}

func BenchmarkInterleaveS32Compare(b *testing.B) {
	for _, blockSize := range []int{4, 16, 64, 256, 4096} {
		left := make([]int64, blockSize)
		right := make([]int64, blockSize)
		for i := range left {
			left[i] = int64((i*7919)%65536 - 32768)
			right[i] = int64((i*1543)%65536 - 32768)
		}
		samples := [][]int64{left, right}
		plane := make([]byte, blockSize*2*4)
		b.Run(strconv.Itoa(blockSize), func(b *testing.B) {
			b.Run("scalar", func(b *testing.B) {
				for b.Loop() {
					interleaveS32Scalar(plane, samples, blockSize, 2)
				}
			})
			b.Run("simd", func(b *testing.B) {
				for b.Loop() {
					interleaveS32StereoSIMD(plane, left, right, blockSize)
				}
			})
		})
	}
}
