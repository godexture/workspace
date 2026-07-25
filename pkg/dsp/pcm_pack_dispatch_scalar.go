//go:build !goexperiment.simd || !amd64

package dsp

func packS32(dst []byte, src [][]int64, blockSize, channels int) {
	packS32Scalar(dst, src, blockSize, channels)
}
