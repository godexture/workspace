//go:build goexperiment.simd && amd64

package encoder

import "simd/archsimd"

func int64x4ShiftRightConstants(shift uint64) (archsimd.Uint64x4, archsimd.Int64x4) {
	topBit := archsimd.BroadcastUint64x4(uint64(1) << 63)
	bias := archsimd.BroadcastInt64x4(int64((uint64(1) << 63) >> shift))
	return topBit, bias
}

func shiftRightInt64x4(value archsimd.Int64x4, shift uint64, topBit archsimd.Uint64x4, bias archsimd.Int64x4) archsimd.Int64x4 {
	return value.AsUint64x4().Xor(topBit).ShiftAllRight(shift).AsInt64x4().Sub(bias)
}
