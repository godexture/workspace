//go:build !goexperiment.simd || !amd64

package mp3

func dctType2(granule []float32, bandCount int) {
	dctType2Scalar(granule, bandCount)
}
