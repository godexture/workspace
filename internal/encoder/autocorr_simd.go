//go:build goexperiment.simd && amd64

package encoder

import (
	"simd/archsimd"

	"github.com/godexture/sdk/dsp"
)

func autocorrelate(values, auto []float64) {
	if dsp.HasAVX2FMA && len(auto) >= 33 {
		autocorrelateSIMD(values, auto)
		return
	}
	autocorrelateScalar(values, auto)
}

func autocorrelateSIMD(values, auto []float64) {
	var lanes [4]float64
	for lag := range auto {
		var acc0, acc1, acc2, acc3 archsimd.Float64x4
		i := lag
		for ; i+16 <= len(values); i += 16 {
			acc0 = archsimd.LoadFloat64x4Slice(values[i:]).MulAdd(
				archsimd.LoadFloat64x4Slice(values[i-lag:]), acc0)
			acc1 = archsimd.LoadFloat64x4Slice(values[i+4:]).MulAdd(
				archsimd.LoadFloat64x4Slice(values[i-lag+4:]), acc1)
			acc2 = archsimd.LoadFloat64x4Slice(values[i+8:]).MulAdd(
				archsimd.LoadFloat64x4Slice(values[i-lag+8:]), acc2)
			acc3 = archsimd.LoadFloat64x4Slice(values[i+12:]).MulAdd(
				archsimd.LoadFloat64x4Slice(values[i-lag+12:]), acc3)
		}
		acc := acc0.Add(acc1).Add(acc2.Add(acc3))
		for ; i+4 <= len(values); i += 4 {
			acc = archsimd.LoadFloat64x4Slice(values[i:]).MulAdd(
				archsimd.LoadFloat64x4Slice(values[i-lag:]), acc)
		}
		acc.StoreSlice(lanes[:])
		sum := lanes[0] + lanes[1] + lanes[2] + lanes[3]
		for ; i < len(values); i++ {
			sum += values[i] * values[i-lag]
		}
		auto[lag] = sum
	}
}
