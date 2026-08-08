//go:build !goexperiment.simd || !amd64

package layer3

func Antialias(granule []float32, bandCount int) {
	antialiasScalar(granule, bandCount)
}
