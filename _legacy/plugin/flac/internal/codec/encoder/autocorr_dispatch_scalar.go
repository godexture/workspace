//go:build !goexperiment.simd || !amd64

package encoder

func autocorrelate(values, auto []float64) {
	autocorrelateScalar(values, auto)
}
