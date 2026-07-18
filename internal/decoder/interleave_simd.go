//go:build goexperiment.simd && amd64

package decoder

import (
	"simd/archsimd"

	"github.com/godexture/sdk/dsp"
)

var interleaveLowInt32LaneIndices = [...]uint32{0, 2, 4, 6, 0, 2, 4, 6}

func interleaveS32(plane []byte, samples [][]int64, blockSize, channels int) {
	if dsp.HasAVX2 && channels == 2 && blockSize >= 4 {
		interleaveS32StereoSIMD(plane, samples[0], samples[1], blockSize)
		return
	}
	interleaveS32Scalar(plane, samples, blockSize, channels)
}

func interleaveS32StereoSIMD(plane []byte, left, right []int64, blockSize int) {
	output := dsp.AsSamples[int32](plane)
	if output == nil {
		interleaveS32StereoScalar(plane, left, right, 0, blockSize)
		return
	}
	indices := archsimd.LoadUint32x8Slice(interleaveLowInt32LaneIndices[:])
	i := 0
	for ; i+4 <= blockSize; i += 4 {
		leftValue := archsimd.LoadInt64x4Slice(left[i:]).AsInt32x8().Permute(indices).GetLo()
		rightValue := archsimd.LoadInt64x4Slice(right[i:]).AsInt32x8().Permute(indices).GetLo()
		var interleaved archsimd.Int32x8
		interleaved = interleaved.SetLo(leftValue.InterleaveLo(rightValue))
		interleaved = interleaved.SetHi(leftValue.InterleaveHi(rightValue))
		interleaved.StoreSlice(output[i*2:])
	}
	interleaveS32StereoScalar(plane, left, right, i, blockSize)
}
