//go:build !goexperiment.simd || !amd64

package encoder

func deinterleaveS32(buffer [][]int64, plane []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	return deinterleaveS32Scalar(buffer, plane, writeStart, samples, channels, minValue, maxValue, bitsPerSample)
}
