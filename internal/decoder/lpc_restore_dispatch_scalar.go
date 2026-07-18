//go:build !goexperiment.simd || !amd64

package decoder

func restoreLPC(samples, coefficients []int64, order, shift int, min, max int64, bitsPerSample int) error {
	return restoreLPCScalar(samples, coefficients, order, shift, min, max, bitsPerSample)
}
