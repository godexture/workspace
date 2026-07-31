//go:build goexperiment.simd && amd64

package encoder

import (
	"simd/archsimd"

	"github.com/godexture/godec/sdk/dsp"
)

var lowInt32LaneIndices = [...]uint32{0, 2, 4, 6, 0, 2, 4, 6}

func windowSamples(samples []int64, window, values []float64, bitsPerSample int) {
	if dsp.HasAVX2 && bitsPerSample <= 32 && window != nil && len(samples) >= 4096 {
		windowSamplesSIMD(samples, window, values)
		return
	}
	windowSamplesScalar(samples, window, values)
}

func windowSamplesSIMD(samples []int64, window, values []float64) {
	indices := archsimd.LoadUint32x8Slice(lowInt32LaneIndices[:])
	i := 0
	for ; i+4 <= len(samples); i += 4 {
		value := archsimd.LoadInt64x4Slice(samples[i:]).AsInt32x8().Permute(indices).GetLo().ConvertToFloat64()
		if window != nil {
			value = value.Mul(archsimd.LoadFloat64x4Slice(window[i:]))
		}
		value.StoreSlice(values[i:])
	}
	if window == nil {
		windowSamplesScalar(samples[i:], nil, values[i:])
		return
	}
	windowSamplesScalar(samples[i:], window[i:], values[i:])
}
