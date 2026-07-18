//go:build !goexperiment.simd || !amd64

package internal

func leftJustifyS16(destination, source []byte, shift uint) {
	leftJustifyS16Scalar(destination, source, shift)
}

func leftJustifyS32(destination, source []byte, shift uint) {
	leftJustifyS32Scalar(destination, source, shift)
}
