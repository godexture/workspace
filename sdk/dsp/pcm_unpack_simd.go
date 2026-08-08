//go:build goexperiment.simd && amd64

package dsp

import "simd/archsimd"

var (
	unpackEvenInt32LaneIndices = [...]uint32{0, 2, 4, 6, 0, 2, 4, 6}
	unpackOddInt32LaneIndices  = [...]uint32{1, 3, 5, 7, 1, 3, 5, 7}
)

func unpackS32(dst [][]int64, src []byte, writeStart, samples, channels int, minValue, maxValue int64, bitsPerSample int) error {
	if HasAVX2() && channels == 2 && samples >= 4 {
		return unpackS32StereoSIMD(dst, src, writeStart, samples, minValue, maxValue, bitsPerSample)
	}
	return unpackS32Scalar(dst, src, writeStart, samples, channels, minValue, maxValue, bitsPerSample)
}

func unpackS32StereoSIMD(dst [][]int64, src []byte, writeStart, samples int, minValue, maxValue int64, bitsPerSample int) error {
	input := AsSamples[int32](src)
	if input == nil {
		return unpackS32Scalar(dst, src, writeStart, samples, 2, minValue, maxValue, bitsPerSample)
	}
	minimum := archsimd.BroadcastInt32x8(int32(minValue))
	maximum := archsimd.BroadcastInt32x8(int32(maxValue))
	evenIndices := archsimd.LoadUint32x8Slice(unpackEvenInt32LaneIndices[:])
	oddIndices := archsimd.LoadUint32x8Slice(unpackOddInt32LaneIndices[:])
	i := 0
	for ; i+4 <= samples; i += 4 {
		values := archsimd.LoadInt32x8Slice(input[i*2:])
		if minimum.Greater(values).Or(values.Greater(maximum)).ToBits() != 0 {
			return unpackS32Scalar(dst, src[i*8:], writeStart+i, samples-i, 2, minValue, maxValue, bitsPerSample)
		}
		values.Permute(evenIndices).GetLo().ExtendToInt64().StoreSlice(dst[0][writeStart+i:])
		values.Permute(oddIndices).GetLo().ExtendToInt64().StoreSlice(dst[1][writeStart+i:])
	}
	return unpackS32Scalar(dst, src[i*8:], writeStart+i, samples-i, 2, minValue, maxValue, bitsPerSample)
}
