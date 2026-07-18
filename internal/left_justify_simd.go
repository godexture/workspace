//go:build goexperiment.simd && amd64

package internal

import (
	"simd/archsimd"

	"github.com/godexture/sdk/dsp"
)

func leftJustifyS16(destination, source []byte, shift uint) {
	if dsp.HasAVX2 {
		leftJustifyS16SIMD(destination, source, shift)
		return
	}
	leftJustifyS16Scalar(destination, source, shift)
}

func leftJustifyS32(destination, source []byte, shift uint) {
	if dsp.HasAVX2 {
		leftJustifyS32SIMD(destination, source, shift)
		return
	}
	leftJustifyS32Scalar(destination, source, shift)
}

func leftJustifyS16SIMD(destination, source []byte, shift uint) {
	input := dsp.AsSamples[int16](source)
	output := dsp.AsSamples[int16](destination)
	if input == nil || output == nil {
		leftJustifyS16Scalar(destination, source, shift)
		return
	}
	length := min(len(input), len(output))
	i := 0
	for ; i+16 <= length; i += 16 {
		archsimd.LoadInt16x16Slice(input[i:]).ShiftAllLeft(uint64(shift)).StoreSlice(output[i:])
	}
	for ; i < length; i++ {
		output[i] = input[i] << shift
	}
}

func leftJustifyS32SIMD(destination, source []byte, shift uint) {
	input := dsp.AsSamples[int32](source)
	output := dsp.AsSamples[int32](destination)
	if input == nil || output == nil {
		leftJustifyS32Scalar(destination, source, shift)
		return
	}
	length := min(len(input), len(output))
	i := 0
	for ; i+8 <= length; i += 8 {
		archsimd.LoadInt32x8Slice(input[i:]).ShiftAllLeft(uint64(shift)).StoreSlice(output[i:])
	}
	for ; i < length; i++ {
		output[i] = input[i] << shift
	}
}
