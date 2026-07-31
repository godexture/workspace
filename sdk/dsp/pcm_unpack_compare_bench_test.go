//go:build goexperiment.simd && amd64

package dsp

import (
	"encoding/binary"
	"strconv"
	"testing"
)

func BenchmarkUnpackS32Compare(b *testing.B) {
	for _, samples := range []int{4, 16, 64, 256, 4096} {
		plane := make([]byte, samples*2*4)
		for i := 0; i < samples*2; i++ {
			binary.LittleEndian.PutUint32(plane[i*4:], uint32(int32((i*7919)%65536-32768)))
		}
		buffer := [][]int64{make([]int64, samples), make([]int64, samples)}
		b.Run(strconv.Itoa(samples), func(b *testing.B) {
			b.Run("scalar", func(b *testing.B) {
				for b.Loop() {
					if err := unpackS32Scalar(buffer, plane, 0, samples, 2, -1<<31, 1<<31-1, 32); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("simd", func(b *testing.B) {
				for b.Loop() {
					if err := unpackS32StereoSIMD(buffer, plane, 0, samples, -1<<31, 1<<31-1, 32); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
