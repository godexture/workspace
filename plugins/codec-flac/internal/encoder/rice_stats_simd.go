//go:build goexperiment.simd && amd64

package encoder

import (
	"simd/archsimd"

	"github.com/godexture/godec/sdk/dsp"
)

const (
	sumMaxSIMDThreshold     = 1024
	foldBatchSIMDThreshold  = 16
	foldSumMaxSIMDThreshold = 1024
)

func sumMaxUint64(values []uint64) (uint64, uint64) {
	if dsp.HasAVX2 && len(values) >= sumMaxSIMDThreshold {
		return sumMaxUint64SIMD(values)
	}
	return sumMaxUint64Scalar(values)
}

func foldResidualBatch(residual []int64, folded []uint64) (uint64, bool) {
	if dsp.HasAVX2 && len(residual) >= foldBatchSIMDThreshold {
		return foldResidualBatchSIMD(residual, folded)
	}
	return foldResidualBatchScalar(residual, folded)
}

func foldSumMax(residual []int64) (uint64, uint64) {
	if dsp.HasAVX2 && len(residual) >= foldSumMaxSIMDThreshold {
		return foldSumMaxSIMD(residual)
	}
	return foldSumMaxScalar(residual)
}

func sumMaxUint64SIMD(values []uint64) (sum, maximum uint64) {
	var sumAcc archsimd.Uint64x4
	var maxAcc archsimd.Int64x4
	i := 0
	for ; i+4 <= len(values); i += 4 {
		value := archsimd.LoadUint64x4Slice(values[i:])
		sumAcc = sumAcc.Add(value)
		maxAcc = maxFoldedUint64x4(maxAcc, value)
	}
	sum, maximum = reduceFoldedUint64x4(sumAcc, maxAcc)
	tailSum, tailMax := sumMaxUint64Scalar(values[i:])
	sum += tailSum
	if tailMax > maximum {
		maximum = tailMax
	}
	return sum, maximum
}

func foldResidualBatchSIMD(residual []int64, folded []uint64) (maximum uint64, ok bool) {
	minimum := archsimd.BroadcastInt64x4(-2147483647)
	maximumBound := archsimd.BroadcastInt64x4(2147483647)
	var maxAcc archsimd.Int64x4
	i := 0
	for ; i+4 <= len(residual); i += 4 {
		value := archsimd.LoadInt64x4Slice(residual[i:])
		if minimum.Greater(value).Or(value.Greater(maximumBound)).ToBits() != 0 {
			return 0, false
		}
		foldedValue := foldInt64x4(value)
		foldedValue.StoreSlice(folded[i:])
		maxAcc = maxFoldedUint64x4(maxAcc, foldedValue)
	}
	maximum = reduceMaxInt64x4(maxAcc)
	tailMax, ok := foldResidualBatchScalar(residual[i:], folded[i:])
	if !ok {
		return 0, false
	}
	if tailMax > maximum {
		maximum = tailMax
	}
	return maximum, true
}

func foldSumMaxSIMD(residual []int64) (sum, maximum uint64) {
	var sumAcc archsimd.Uint64x4
	var maxAcc archsimd.Int64x4
	i := 0
	for ; i+4 <= len(residual); i += 4 {
		foldedValue := foldInt64x4(archsimd.LoadInt64x4Slice(residual[i:]))
		sumAcc = sumAcc.Add(foldedValue)
		maxAcc = maxFoldedUint64x4(maxAcc, foldedValue)
	}
	sum, maximum = reduceFoldedUint64x4(sumAcc, maxAcc)
	tailSum, tailMax := foldSumMaxScalar(residual[i:])
	sum += tailSum
	if tailMax > maximum {
		maximum = tailMax
	}
	return sum, maximum
}

func foldInt64x4(value archsimd.Int64x4) archsimd.Uint64x4 {
	var zero archsimd.Int64x4
	sign := zero.Greater(value).ToInt64x4()
	return value.ShiftAllLeft(1).Xor(sign).AsUint64x4()
}

func maxFoldedUint64x4(current archsimd.Int64x4, value archsimd.Uint64x4) archsimd.Int64x4 {
	signed := value.AsInt64x4()
	return signed.Merge(current, signed.Greater(current))
}

func reduceFoldedUint64x4(sumAcc archsimd.Uint64x4, maxAcc archsimd.Int64x4) (sum, maximum uint64) {
	var lanes [4]uint64
	sumAcc.StoreSlice(lanes[:])
	for _, value := range lanes {
		sum += value
	}
	return sum, reduceMaxInt64x4(maxAcc)
}

func reduceMaxInt64x4(value archsimd.Int64x4) uint64 {
	var lanes [4]uint64
	value.AsUint64x4().StoreSlice(lanes[:])
	maximum := lanes[0]
	for _, lane := range lanes[1:] {
		if lane > maximum {
			maximum = lane
		}
	}
	return maximum
}
