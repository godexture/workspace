//go:build !goexperiment.simd || !amd64

package encoder

func windowSamples(samples []int64, window, values []float64, bitsPerSample int) {
	windowSamplesScalar(samples, window, values)
}
