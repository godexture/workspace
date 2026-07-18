//go:build goexperiment.simd && amd64

package encoder

import (
	"simd/archsimd"

	"github.com/godexture/sdk/dsp"
)

var (
	evenInt32LaneIndices = [...]uint32{0, 2, 4, 6, 0, 2, 4, 6}
	oddInt32LaneIndices  = [...]uint32{1, 3, 5, 7, 1, 3, 5, 7}
)

func deinterleaveS32(buffer [][]int64, plane []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	if dsp.HasAVX2 && channels == 2 && samples >= 4 {
		return deinterleaveS32StereoSIMD(buffer, plane, writeStart, samples, minValue, maxValue, bitsPerSample)
	}
	return deinterleaveS32Scalar(buffer, plane, writeStart, samples, channels, minValue, maxValue, bitsPerSample)
}

func deinterleaveS32StereoSIMD(buffer [][]int64, plane []byte, writeStart, samples int, minValue, maxValue int64, bitsPerSample int) error {
	input := dsp.AsSamples[int32](plane)
	if input == nil {
		return deinterleaveS32Scalar(buffer, plane, writeStart, samples, 2, minValue, maxValue, bitsPerSample)
	}
	minimum := archsimd.BroadcastInt32x8(int32(minValue))
	maximum := archsimd.BroadcastInt32x8(int32(maxValue))
	evenIndices := archsimd.LoadUint32x8Slice(evenInt32LaneIndices[:])
	oddIndices := archsimd.LoadUint32x8Slice(oddInt32LaneIndices[:])
	i := 0
	for ; i+4 <= samples; i += 4 {
		values := archsimd.LoadInt32x8Slice(input[i*2:])
		if minimum.Greater(values).Or(values.Greater(maximum)).ToBits() != 0 {
			return deinterleaveS32Scalar(buffer, plane[i*8:], writeStart+i, samples-i, 2, minValue, maxValue, bitsPerSample)
		}
		values.Permute(evenIndices).GetLo().ExtendToInt64().StoreSlice(buffer[0][writeStart+i:])
		values.Permute(oddIndices).GetLo().ExtendToInt64().StoreSlice(buffer[1][writeStart+i:])
	}
	return deinterleaveS32Scalar(buffer, plane[i*8:], writeStart+i, samples-i, 2, minValue, maxValue, bitsPerSample)
}
