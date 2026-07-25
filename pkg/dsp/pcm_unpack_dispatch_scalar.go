//go:build !goexperiment.simd || !amd64

package dsp

func unpackS32(dst [][]int64, src []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	return unpackS32Scalar(dst, src, writeStart, samples, channels, minValue, maxValue, bitsPerSample)
}
