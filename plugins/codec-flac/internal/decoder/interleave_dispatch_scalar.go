//go:build !goexperiment.simd || !amd64

package decoder

func interleaveS32(plane []byte, samples [][]int64, blockSize, channels int) {
	interleaveS32Scalar(plane, samples, blockSize, channels)
}
