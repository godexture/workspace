//go:build goexperiment.simd && amd64

package dsp

import "simd/archsimd"

func ConvertF32ToS16(destination []int16, source []float32) {
	if HasAVX2() {
		convertF32ToS16SIMD(destination, source)
		return
	}
	convertF32ToS16Scalar(destination, source)
}

func convertF32ToS16SIMD(destination []int16, source []float32) {
	length := min(len(destination), len(source))
	negativeOne := archsimd.BroadcastFloat32x8(-1)
	one := archsimd.BroadcastFloat32x8(1)
	zero := archsimd.BroadcastFloat32x8(0)
	negativeScale := archsimd.BroadcastFloat32x8(32768)
	positiveScale := archsimd.BroadcastFloat32x8(32767)
	laneOrder := [...]uint32{0, 1, 4, 5, 2, 3, 6, 7}
	lanes := archsimd.LoadUint32x8Slice(laneOrder[:])

	i := 0
	for ; i+16 <= length; i += 16 {
		first := convertF32x8ToInt32(source[i:], negativeOne, one, zero, negativeScale, positiveScale)
		second := convertF32x8ToInt32(source[i+8:], negativeOne, one, zero, negativeScale, positiveScale)
		packed := first.SaturateToInt16ConcatGrouped(second)
		packed.AsInt32x8().Permute(lanes).AsInt16x16().StoreSlice(destination[i:])
	}
	convertF32ToS16Scalar(destination[i:length], source[i:length])
}

func convertF32x8ToInt32(source []float32, negativeOne, one, zero, negativeScale, positiveScale archsimd.Float32x8) archsimd.Int32x8 {
	value := archsimd.LoadFloat32x8Slice(source)
	value = value.Max(negativeOne).Min(one)
	scale := negativeScale.Merge(positiveScale, value.Less(zero))
	return value.Mul(scale).ConvertToInt32()
}
