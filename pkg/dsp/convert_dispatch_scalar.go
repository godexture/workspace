//go:build !goexperiment.simd || !amd64

package dsp

func ConvertF32ToS16(destination []int16, source []float32) {
	convertF32ToS16Scalar(destination, source)
}
