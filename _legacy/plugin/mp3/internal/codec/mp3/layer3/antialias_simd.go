//go:build goexperiment.simd && amd64

package layer3

import (
	"simd/archsimd"

	"github.com/godexture/godec/sdk/dsp"
)

func Antialias(granule []float32, bandCount int) {
	if dsp.HasAVX2 && SamplesPerSubBand == 18 {
		antialiasSIMD(granule, bandCount)
		return
	}
	antialiasScalar(granule, bandCount)
}

func antialiasSIMD(granule []float32, bandCount int) {
	cs := archsimd.LoadFloat32x8Slice(aliasReductionCS[:])
	ca := archsimd.LoadFloat32x8Slice(aliasReductionCA[:])
	reverseOrder := [...]uint32{7, 6, 5, 4, 3, 2, 1, 0}
	reverse := archsimd.LoadUint32x8Slice(reverseOrder[:])
	bandOffset := 0
	for ; bandCount > 0; bandCount-- {
		upper := archsimd.LoadFloat32x8Slice(granule[bandOffset+18:])
		lower := archsimd.LoadFloat32x8Slice(granule[bandOffset+10:]).Permute(reverse)
		upper.Mul(cs).Sub(lower.Mul(ca)).StoreSlice(granule[bandOffset+18:])
		upper.Mul(ca).Add(lower.Mul(cs)).Permute(reverse).StoreSlice(granule[bandOffset+10:])
		bandOffset += 18
	}
}
