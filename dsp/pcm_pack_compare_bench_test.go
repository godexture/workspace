//go:build goexperiment.simd && amd64

package dsp

import (
	"strconv"
	"testing"
)

func BenchmarkPackS32Compare(b *testing.B) {
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
					packS32Scalar(plane, samples, blockSize, 2)
				}
			})
			b.Run("simd", func(b *testing.B) {
				for b.Loop() {
					packS32StereoSIMD(plane, left, right, blockSize)
				}
			})
		})
	}
}
