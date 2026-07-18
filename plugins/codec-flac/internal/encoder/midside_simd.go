//go:build goexperiment.simd && amd64

package encoder

import (
	"simd/archsimd"

	"github.com/godexture/sdk/dsp"
)

func computeMidSide(left, right, mid, side []int64) {
	if dsp.HasAVX2 && len(left) >= 1024 {
		computeMidSideSIMD(left, right, mid, side)
		return
	}
	computeMidSideScalar(left, right, mid, side)
}

func computeMidSideSIMD(left, right, mid, side []int64) {
	topBit, bias := int64x4ShiftRightConstants(1)
	i := 0
	for ; i+16 <= len(left); i += 16 {
		computeMidSide4(left[i:], right[i:], mid[i:], side[i:], topBit, bias)
		computeMidSide4(left[i+4:], right[i+4:], mid[i+4:], side[i+4:], topBit, bias)
		computeMidSide4(left[i+8:], right[i+8:], mid[i+8:], side[i+8:], topBit, bias)
		computeMidSide4(left[i+12:], right[i+12:], mid[i+12:], side[i+12:], topBit, bias)
	}
	for ; i+4 <= len(left); i += 4 {
		computeMidSide4(left[i:], right[i:], mid[i:], side[i:], topBit, bias)
	}
	computeMidSideScalar(left[i:], right[i:], mid[i:], side[i:])
}

func computeMidSide4(left, right, mid, side []int64, topBit archsimd.Uint64x4, bias archsimd.Int64x4) {
	leftValue := archsimd.LoadInt64x4Slice(left)
	rightValue := archsimd.LoadInt64x4Slice(right)
	leftValue.Add(rightValue).AsUint64x4().Xor(topBit).ShiftAllRight(1).
		AsInt64x4().Sub(bias).StoreSlice(mid)
	leftValue.Sub(rightValue).StoreSlice(side)
}
