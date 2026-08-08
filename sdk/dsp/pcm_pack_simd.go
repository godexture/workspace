//go:build goexperiment.simd && amd64

package dsp

import "simd/archsimd"

var packLowInt32LaneIndices = [...]uint32{0, 2, 4, 6, 0, 2, 4, 6}

func packS32(dst []byte, src [][]int64, blockSize, channels int) {
	if HasAVX2() && channels == 2 && blockSize >= 4 {
		packS32StereoSIMD(dst, src[0], src[1], blockSize)
		return
	}
	packS32Scalar(dst, src, blockSize, channels)
}

func packS32StereoSIMD(dst []byte, left, right []int64, blockSize int) {
	output := AsSamples[int32](dst)
	if output == nil {
		packS32StereoScalar(dst, left, right, 0, blockSize)
		return
	}
	indices := archsimd.LoadUint32x8Slice(packLowInt32LaneIndices[:])
	i := 0
	for ; i+4 <= blockSize; i += 4 {
		leftValue := archsimd.LoadInt64x4Slice(left[i:]).AsInt32x8().Permute(indices).GetLo()
		rightValue := archsimd.LoadInt64x4Slice(right[i:]).AsInt32x8().Permute(indices).GetLo()
		var interleaved archsimd.Int32x8
		interleaved = interleaved.SetLo(leftValue.InterleaveLo(rightValue))
		interleaved = interleaved.SetHi(leftValue.InterleaveHi(rightValue))
		interleaved.StoreSlice(output[i*2:])
	}
	packS32StereoScalar(dst, left, right, i, blockSize)
}
