//go:build goexperiment.simd && amd64

package encoder

import (
	"simd/archsimd"
	"unsafe"

	"github.com/godexture/godec/sdk/dsp"
)

func autocorrelate(values, auto []float64) {
	if dsp.HasAVX2FMA {
		autocorrelateSIMD(values, auto)
		return
	}
	autocorrelateScalar(values, auto)
}

// loadFloat64x4At requires index >= 0 and index+4 <= len(values).
func loadFloat64x4At(base unsafe.Pointer, index int) archsimd.Float64x4 {
	return archsimd.LoadFloat64x4((*[4]float64)(unsafe.Add(base, uintptr(index)*8)))
}

func autocorrelateSIMD(values, auto []float64) {
	var lanes [4]float64
	base := unsafe.Pointer(unsafe.SliceData(values))
	for lag := range auto {
		var acc0, acc1, acc2, acc3 archsimd.Float64x4
		i := lag
		// i >= lag and the loop guards keep both input vectors in values.
		for ; i+16 <= len(values); i += 16 {
			acc0 = loadFloat64x4At(base, i).MulAdd(
				loadFloat64x4At(base, i-lag), acc0)
			acc1 = loadFloat64x4At(base, i+4).MulAdd(
				loadFloat64x4At(base, i-lag+4), acc1)
			acc2 = loadFloat64x4At(base, i+8).MulAdd(
				loadFloat64x4At(base, i-lag+8), acc2)
			acc3 = loadFloat64x4At(base, i+12).MulAdd(
				loadFloat64x4At(base, i-lag+12), acc3)
		}
		acc := acc0.Add(acc1).Add(acc2.Add(acc3))
		for ; i+4 <= len(values); i += 4 {
			acc = loadFloat64x4At(base, i).MulAdd(
				loadFloat64x4At(base, i-lag), acc)
		}
		acc.StoreSlice(lanes[:])
		sum := lanes[0] + lanes[1] + lanes[2] + lanes[3]
		for ; i < len(values); i++ {
			sum += values[i] * values[i-lag]
		}
		auto[lag] = sum
	}
}
