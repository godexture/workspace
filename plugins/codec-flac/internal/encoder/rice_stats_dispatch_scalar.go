//go:build !goexperiment.simd || !amd64

package encoder

func sumMaxUint64(values []uint64) (uint64, uint64) {
	return sumMaxUint64Scalar(values)
}

func foldResidualBatch(residual []int64, folded []uint64) (uint64, bool) {
	return foldResidualBatchScalar(residual, folded)
}

func foldSumMax(residual []int64) (uint64, uint64) {
	return foldSumMaxScalar(residual)
}
