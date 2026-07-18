//go:build !goexperiment.simd || !amd64

package encoder

func fixedResidual(samples []int64, order int) []int64 {
	return fixedResidualScalar(samples, order)
}

func lpcResidual(samples []int64, order int, coefficients []int64, shift, bitsPerSample int) []int64 {
	return lpcResidualScalar(samples, order, coefficients, shift)
}
