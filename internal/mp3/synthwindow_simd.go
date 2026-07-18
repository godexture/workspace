//go:build goexperiment.simd && amd64

package mp3

import (
	"simd/archsimd"

	"github.com/godexture/sdk/dsp"
)

func synthWindow(workspace []float32, zLineOffset, index int, window []float32) ([4]float32, [4]float32) {
	if dsp.HasAVX2 {
		return synthWindowSIMD(workspace, zLineOffset, index, window)
	}
	return synthWindowScalar(workspace, zLineOffset, index, window)
}

func synthWindowSIMD(workspace []float32, zLineOffset, index int, window []float32) (a, b [4]float32) {
	var accumulatorA, accumulatorB archsimd.Float32x4
	for k := 0; k < 8; k++ {
		vZeroIndex := zLineOffset + 4*index - k*64
		vYIndex := zLineOffset + 4*index - (15-k)*64
		vZero := archsimd.LoadFloat32x4Slice(workspace[vZeroIndex:])
		vY := archsimd.LoadFloat32x4Slice(workspace[vYIndex:])
		w0 := archsimd.BroadcastFloat32x4(window[2*k])
		w1 := archsimd.BroadcastFloat32x4(window[2*k+1])
		termB := vZero.Mul(w1).Add(vY.Mul(w0))
		termA := vZero.Mul(w0).Sub(vY.Mul(w1))
		if k&1 != 0 {
			termA = vY.Mul(w1).Sub(vZero.Mul(w0))
		}
		if k == 0 {
			accumulatorA = termA
			accumulatorB = termB
		} else {
			accumulatorA = accumulatorA.Add(termA)
			accumulatorB = accumulatorB.Add(termB)
		}
	}
	accumulatorA.StoreSlice(a[:])
	accumulatorB.StoreSlice(b[:])
	return a, b
}
