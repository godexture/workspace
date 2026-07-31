//go:build goexperiment.simd && amd64

package encoder

import (
	"simd/archsimd"
	"unsafe"

	"github.com/godexture/godec/sdk/dsp"
)

func fixedResidual(samples []int64, order int) []int64 {
	if dsp.HasAVX2 && order >= 0 && order <= 4 && len(samples)-order >= 4 {
		return fixedResidualSIMD(samples, order)
	}
	return fixedResidualScalar(samples, order)
}

func lpcResidual(samples []int64, order int, coefficients []int64, shift, bitsPerSample int) []int64 {
	// Below order 20, lpcResidualSIMD's per-call setup (building the
	// coefficientVectors table) dominates and it loses to scalar by 2-6x;
	// scalar stays competitive or better up to roughly order 18-20 depending
	// on system load, but SIMD reliably wins from order 20 on.
	if dsp.HasAVX2 && bitsPerSample <= 32 && order >= 20 && order <= 32 && len(samples)-order >= 4 {
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

	base := unsafe.Pointer(unsafe.SliceData(samples))
	i := order
	// i >= order and i+4 <= len(samples) keep every order-relative load in samples.
	for ; i+4 <= len(samples); i += 4 {
		current := loadInt64x4At(base, i)
		s1 := loadInt64x4At(base, i-1)
		var residual archsimd.Int64x4
		switch order {
		case 1:
			residual = current.Sub(s1)
		case 2:
			s2 := loadInt64x4At(base, i-2)
			residual = current.Sub(s1.ShiftAllLeft(1)).Add(s2)
		case 3:
			s2 := loadInt64x4At(base, i-2)
			s3 := loadInt64x4At(base, i-3)
			residual = current.Sub(s1.ShiftAllLeft(1)).Sub(s1).
				Add(s2.ShiftAllLeft(1)).Add(s2).Sub(s3)
		case 4:
			s2 := loadInt64x4At(base, i-2)
			s3 := loadInt64x4At(base, i-3)
			s4 := loadInt64x4At(base, i-4)
			residual = current.Sub(s1.ShiftAllLeft(2)).
				Add(s2.ShiftAllLeft(2)).Add(s2.ShiftAllLeft(1)).
				Sub(s3.ShiftAllLeft(2)).Add(s4)
		}
		residual.StoreSlice(result[i-order:])
	}
	for ; i < len(samples); i++ {
		result[i-order] = samples[i] - fixedPredictionAt(base, i, order)
	}
	return result
}

// loadInt64x4At loads 4 consecutive int64s starting at samples[index] without
// a bounds check. The caller must guarantee index >= 0 and index+4 <= len(samples).
func loadInt64x4At(base unsafe.Pointer, index int) archsimd.Int64x4 {
	return archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(base, uintptr(index)*8)))
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
	base := unsafe.Pointer(unsafe.SliceData(samples))

	i := order
	// Process 8 samples per iteration with 2 independent accumulators per
	// output group: this breaks the multiply-add dependency chain so the
	// CPU can keep more FMA/mul ports busy, and unsafe loads skip the
	// per-access bounds check that the dynamic coefficient-index offsets
	// would otherwise force.
	for ; i+8 <= len(samples); i += 8 {
		var sumA0, sumA1, sumB0, sumB1 archsimd.Int64x4
		j := 0
		for ; j+2 <= order; j += 2 {
			c0, c1 := coefficientVectors[j], coefficientVectors[j+1]
			sumA0 = sumA0.Add(loadInt64x4At(base, i-1-j).AsInt32x8().MulEvenWiden(c0))
			sumA1 = sumA1.Add(loadInt64x4At(base, i-2-j).AsInt32x8().MulEvenWiden(c1))
			sumB0 = sumB0.Add(loadInt64x4At(base, i+3-j).AsInt32x8().MulEvenWiden(c0))
			sumB1 = sumB1.Add(loadInt64x4At(base, i+2-j).AsInt32x8().MulEvenWiden(c1))
		}
		if j < order {
			c := coefficientVectors[j]
			sumA0 = sumA0.Add(loadInt64x4At(base, i-1-j).AsInt32x8().MulEvenWiden(c))
			sumB0 = sumB0.Add(loadInt64x4At(base, i+3-j).AsInt32x8().MulEvenWiden(c))
		}
		predictionA := shiftRightInt64x4(sumA0.Add(sumA1), shiftAmount, topBit, bias)
		predictionB := shiftRightInt64x4(sumB0.Add(sumB1), shiftAmount, topBit, bias)
		loadInt64x4At(base, i).Sub(predictionA).StoreSlice(result[i-order:])
		loadInt64x4At(base, i+4).Sub(predictionB).StoreSlice(result[i-order+4:])
	}

	for ; i+4 <= len(samples); i += 4 {
		var sum archsimd.Int64x4
		for j := range coefficients {
			values := loadInt64x4At(base, i-1-j)
			sum = sum.Add(values.AsInt32x8().MulEvenWiden(coefficientVectors[j]))
		}
		prediction := shiftRightInt64x4(sum, shiftAmount, topBit, bias)
		loadInt64x4At(base, i).Sub(prediction).StoreSlice(result[i-order:])
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
