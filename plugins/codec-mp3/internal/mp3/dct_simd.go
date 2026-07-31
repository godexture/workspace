//go:build goexperiment.simd && amd64

package mp3

import (
	"simd/archsimd"

	"github.com/godexture/godec/sdk/dsp"
)

func dctType2(granule []float32, bandCount int) {
	if dsp.HasAVX2 {
		dctType2SIMD(granule, bandCount)
		return
	}
	dctType2Scalar(granule, bandCount)
}

// dctType2Constants holds every coefficient dctType2SIMD needs, broadcast to
// all 8 lanes once per call. Unlike synthWindow's per-index window table,
// every one of these is the same for all bandCount columns, so hoisting the
// broadcasts out of the column loop (instead of redoing them per column, or
// worse per lane group) carries none of the per-tap table-lookup overhead
// that made batching synthWindow a net loss.
type dctType2Constants struct {
	cosine                                   [24]archsimd.Float32x8
	invSqrt2, w1, w2, w3, w4, w5, w6, w7, w8 archsimd.Float32x8
}

func newDCTType2Constants() dctType2Constants {
	var c dctType2Constants
	for i, v := range dctType2CosineCoefficients {
		c.cosine[i] = archsimd.BroadcastFloat32x8(v)
	}
	c.invSqrt2 = archsimd.BroadcastFloat32x8(0.70710677)
	c.w1 = archsimd.BroadcastFloat32x8(0.198912367)
	c.w2 = archsimd.BroadcastFloat32x8(0.382683432)
	c.w3 = archsimd.BroadcastFloat32x8(0.50979561)
	c.w4 = archsimd.BroadcastFloat32x8(0.54119611)
	c.w5 = archsimd.BroadcastFloat32x8(0.60134488)
	c.w6 = archsimd.BroadcastFloat32x8(0.89997619)
	c.w7 = archsimd.BroadcastFloat32x8(1.30656302)
	c.w8 = archsimd.BroadcastFloat32x8(2.56291556)
	return c
}

// dctType2SIMD processes 8 columns of dctType2Scalar at a time: column k
// lives at granule[k], granule[k+18], ... so 8 consecutive columns are 8
// contiguous floats at each of those offsets, letting every scalar
// granule[idx] access become one LoadFloat32x8Slice/StoreSlice. Every
// arithmetic step mirrors dctType2Scalar's operand order exactly (same
// add/sub/mul pairing, no reassociation), so results are bit-for-bit
// identical -- see dct_simd_test.go.
func dctType2SIMD(granule []float32, bandCount int) {
	constants := newDCTType2Constants()
	k := 0
	for ; k+8 <= bandCount; k += 8 {
		dctType2Chunk(granule, k, &constants)
	}
	if k < bandCount {
		dctType2Scalar(granule[k:], bandCount-k)
	}
}

func dctType2Chunk(granule []float32, k int, c *dctType2Constants) {
	var temp [4][8]archsimd.Float32x8

	for i := 0; i < 8; i++ {
		x0 := archsimd.LoadFloat32x8Slice(granule[k+i*18 : k+i*18+8])
		x1 := archsimd.LoadFloat32x8Slice(granule[k+(15-i)*18 : k+(15-i)*18+8])
		x2 := archsimd.LoadFloat32x8Slice(granule[k+(16+i)*18 : k+(16+i)*18+8])
		x3 := archsimd.LoadFloat32x8Slice(granule[k+(31-i)*18 : k+(31-i)*18+8])
		t0 := x0.Add(x3)
		t1 := x1.Add(x2)
		t2 := x1.Sub(x2).Mul(c.cosine[3*i+0])
		t3 := x0.Sub(x3).Mul(c.cosine[3*i+1])
		temp[0][i] = t0.Add(t1)
		temp[1][i] = t0.Sub(t1).Mul(c.cosine[3*i+2])
		temp[2][i] = t3.Add(t2)
		temp[3][i] = t3.Sub(t2).Mul(c.cosine[3*i+2])
	}
	for i := 0; i < 4; i++ {
		x0 := temp[i][0]
		x1 := temp[i][1]
		x2 := temp[i][2]
		x3 := temp[i][3]
		x4 := temp[i][4]
		x5 := temp[i][5]
		x6 := temp[i][6]
		x7 := temp[i][7]

		xtTemporary := x0.Sub(x7)
		x0 = x0.Add(x7)
		x7 = x1.Sub(x6)
		x1 = x1.Add(x6)
		x6 = x2.Sub(x5)
		x2 = x2.Add(x5)
		x5 = x3.Sub(x4)
		x3 = x3.Add(x4)
		x4 = x0.Sub(x3)
		x0 = x0.Add(x3)
		x3 = x1.Sub(x2)
		x1 = x1.Add(x2)
		temp[i][0] = x0.Add(x1)
		temp[i][4] = x0.Sub(x1).Mul(c.invSqrt2)
		x5 = x5.Add(x6)
		x6 = x6.Add(x7).Mul(c.invSqrt2)
		x7 = x7.Add(xtTemporary)
		x3 = x3.Add(x4).Mul(c.invSqrt2)
		x5 = x5.Sub(x7.Mul(c.w1))
		x7 = x7.Add(x5.Mul(c.w2))
		x5 = x5.Sub(x7.Mul(c.w1))
		x0 = xtTemporary.Sub(x6)
		xtTemporary = xtTemporary.Add(x6)
		temp[i][1] = xtTemporary.Add(x7).Mul(c.w3)
		temp[i][2] = x4.Add(x3).Mul(c.w4)
		temp[i][3] = x0.Sub(x5).Mul(c.w5)
		temp[i][5] = x0.Add(x5).Mul(c.w6)
		temp[i][6] = x4.Sub(x3).Mul(c.w7)
		temp[i][7] = xtTemporary.Sub(x7).Mul(c.w8)
	}
	yIndex := k
	for i := 0; i < 7; i++ {
		temp[0][i].StoreSlice(granule[yIndex+0*18 : yIndex+0*18+8])
		temp[2][i].Add(temp[3][i]).Add(temp[3][i+1]).StoreSlice(granule[yIndex+1*18 : yIndex+1*18+8])
		temp[1][i].Add(temp[1][i+1]).StoreSlice(granule[yIndex+2*18 : yIndex+2*18+8])
		temp[2][i+1].Add(temp[3][i]).Add(temp[3][i+1]).StoreSlice(granule[yIndex+3*18 : yIndex+3*18+8])
		yIndex += 4 * 18
	}
	temp[0][7].StoreSlice(granule[yIndex+0*18 : yIndex+0*18+8])
	temp[2][7].Add(temp[3][7]).StoreSlice(granule[yIndex+1*18 : yIndex+1*18+8])
	temp[1][7].StoreSlice(granule[yIndex+2*18 : yIndex+2*18+8])
	temp[3][7].StoreSlice(granule[yIndex+3*18 : yIndex+3*18+8])
}
