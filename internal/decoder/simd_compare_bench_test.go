//go:build goexperiment.simd && amd64

package decoder

import (
	"strconv"
	"testing"
)

func BenchmarkRestoreLPCCompare(b *testing.B) {
	const length = 4096
	for _, order := range []int{4, 8, 16, 32} {
		coefficients := make([]int64, order)
		for i := range coefficients {
			coefficients[i] = int64(i%5 - 2)
		}
		decoded := make([]int64, length)
		for i := range decoded {
			decoded[i] = int64((i*7919)%65536 - 32768)
		}
		encoded := encodeLPCResiduals(decoded, coefficients, order, 7)
		samples := make([]int64, length)
		b.Run("order-"+strconv.Itoa(order), func(b *testing.B) {
			b.Run("scalar", func(b *testing.B) {
				for b.Loop() {
					copy(samples, encoded)
					if err := restoreLPCScalar(samples, coefficients, order, 7, -1<<31, 1<<31-1, 32); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("simd", func(b *testing.B) {
				for b.Loop() {
					copy(samples, encoded)
					if err := restoreLPCSIMD(samples, coefficients, order, 7, -1<<31, 1<<31-1, 32); err != nil {
						b.Fatal(err)
					}
				}
			})
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
