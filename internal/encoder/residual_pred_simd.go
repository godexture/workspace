//go:build goexperiment.simd && amd64

package encoder

import (
	"simd/archsimd"

	"github.com/godexture/sdk/dsp"
)

func fixedResidual(samples []int64, order int) []int64 {
	if dsp.HasAVX2 && order >= 0 && order <= 4 && len(samples)-order >= 4 {
		return fixedResidualSIMD(samples, order)
	}
	return fixedResidualScalar(samples, order)
}

func lpcResidual(samples []int64, order int, coefficients []int64, shift, bitsPerSample int) []int64 {
	if dsp.HasAVX2 && bitsPerSample <= 32 && order > 0 && order <= 32 && len(samples)-order >= 4 {
		return lpcResidualSIMD(samples, order, coefficients, shift)
	}
	return lpcResidualScalar(samples, order, coefficients, shift)
}

func fixedResidualSIMD(samples []int64, order int) []int64 {
	result := getResidualBuffer(len(samples) - order)
	if order == 0 {
		copy(result, samples)
		return result
	}

	i := order
	for ; i+4 <= len(samples); i += 4 {
		current := archsimd.LoadInt64x4Slice(samples[i:])
		s1 := archsimd.LoadInt64x4Slice(samples[i-1:])
		var residual archsimd.Int64x4
		switch order {
		case 1:
			residual = current.Sub(s1)
		case 2:
			s2 := archsimd.LoadInt64x4Slice(samples[i-2:])
			residual = current.Sub(s1.ShiftAllLeft(1)).Add(s2)
		case 3:
			s2 := archsimd.LoadInt64x4Slice(samples[i-2:])
			s3 := archsimd.LoadInt64x4Slice(samples[i-3:])
			residual = current.Sub(s1.ShiftAllLeft(1)).Sub(s1).
				Add(s2.ShiftAllLeft(1)).Add(s2).Sub(s3)
		case 4:
			s2 := archsimd.LoadInt64x4Slice(samples[i-2:])
			s3 := archsimd.LoadInt64x4Slice(samples[i-3:])
			s4 := archsimd.LoadInt64x4Slice(samples[i-4:])
			residual = current.Sub(s1.ShiftAllLeft(2)).
				Add(s2.ShiftAllLeft(2)).Add(s2.ShiftAllLeft(1)).
				Sub(s3.ShiftAllLeft(2)).Add(s4)
		}
		residual.StoreSlice(result[i-order:])
	}
	for ; i < len(samples); i++ {
		result[i-order] = samples[i] - fixedPrediction(samples, i, order)
	}
	return result
}

func lpcResidualSIMD(samples []int64, order int, coefficients []int64, shift int) []int64 {
	coefficients = coefficients[:order:order]
	result := getResidualBuffer(len(samples) - order)
	var coefficientVectors [32]archsimd.Int32x8
	for i, coefficient := range coefficients {
		coefficientVectors[i] = archsimd.BroadcastInt64x4(coefficient).AsInt32x8()
	}
	shiftAmount := uint64(shift)
	topBit, bias := int64x4ShiftRightConstants(shiftAmount)

	i := order
	for ; i+4 <= len(samples); i += 4 {
		var sum archsimd.Int64x4
		for j := range coefficients {
			values := archsimd.LoadInt64x4Slice(samples[i-1-j:])
			sum = sum.Add(values.AsInt32x8().MulEvenWiden(coefficientVectors[j]))
		}
		prediction := shiftRightInt64x4(sum, shiftAmount, topBit, bias)
		archsimd.LoadInt64x4Slice(samples[i:]).Sub(prediction).StoreSlice(result[i-order:])
	}
	for ; i < len(samples); i++ {
		var sum int64
		for j, coefficient := range coefficients {
			sum += coefficient * samples[i-1-j]
		}
		result[i-order] = samples[i] - (sum >> uint(shift))
	}
	return result
}
