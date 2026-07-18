//go:build !goexperiment.simd || !amd64

package encoder

func computeMidSide(left, right, mid, side []int64) {
	computeMidSideScalar(left, right, mid, side)
}
