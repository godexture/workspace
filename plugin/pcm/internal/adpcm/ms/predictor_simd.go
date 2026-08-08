//go:build goexperiment.simd && amd64

package msadpcm

import (
	"simd/archsimd"

	"github.com/godexture/godec/plugin/pcm/internal/adpcm/param"
	"github.com/godexture/godec/sdk/dsp"
)

func findBestPredictor(chunkSamples []int16, samplesPerBlock, step, offset int, coefficients []param.Coefficient) int {
	if dsp.HasAVX2 && len(coefficients) > 0 && len(coefficients) <= 8 && samplesPerBlock >= 2 {
		return findBestPredictorSIMD(chunkSamples, samplesPerBlock, step, offset, coefficients)
	}
	return findBestPredictorScalar(chunkSamples, samplesPerBlock, step, offset, coefficients)
}

func findBestPredictorSIMD(chunkSamples []int16, samplesPerBlock, step, offset int, coefficients []param.Coefficient) int {
	var coefficient1, coefficient2 [8]int32
	for i := range coefficient1 {
		coefficient1[i] = int32(coefficients[0].Coeff1)
		coefficient2[i] = int32(coefficients[0].Coeff2)
	}
	for i, coefficient := range coefficients {
		coefficient1[i] = int32(coefficient.Coeff1)
		coefficient2[i] = int32(coefficient.Coeff2)
	}
	c1 := archsimd.LoadInt32x8Slice(coefficient1[:])
	c2 := archsimd.LoadInt32x8Slice(coefficient2[:])

	sample2 := chunkSamples[offset]
	sample1 := chunkSamples[step+offset]
	initialDelta := int32(abs(int(sample1) - int(sample2)))
	if initialDelta < 16 {
		initialDelta = 16
	}
	s1 := archsimd.BroadcastInt32x8(int32(sample1))
	s2 := archsimd.BroadcastInt32x8(int32(sample2))
	delta := archsimd.BroadcastInt32x8(initialDelta)
	var errorSum archsimd.Int32x8

	zero := archsimd.BroadcastInt32x8(0)
	three := archsimd.BroadcastInt32x8(3)
	seven := archsimd.BroadcastInt32x8(7)
	negativeEight := archsimd.BroadcastInt32x8(-8)
	sixteen := archsimd.BroadcastInt32x8(16)
	minSample := archsimd.BroadcastInt32x8(-32768)
	maxSample := archsimd.BroadcastInt32x8(32767)
	factors := [...]int32{230, 307, 409, 512, 614, 768, 768, 768}
	factorLUT := archsimd.LoadInt32x8Slice(factors[:])

	for sampleIndex := 2; sampleIndex < samplesPerBlock; sampleIndex++ {
		target := archsimd.BroadcastInt32x8(int32(chunkSamples[sampleIndex*step+offset]))
		prediction := truncDiv256SIMD(s1.Mul(c1).Add(s2.Mul(c2)))
		difference := target.Sub(prediction)
		half := delta.ShiftAllRight(1)
		bias := zero.Sub(half).Merge(half, difference.Less(zero))
		biased := difference.Add(bias)

		low := biased.GetLo().ConvertToFloat64().Div(delta.GetLo().ConvertToFloat64()).ConvertToInt32()
		high := biased.GetHi().ConvertToFloat64().Div(delta.GetHi().ConvertToFloat64()).ConvertToInt32()
		var nybble archsimd.Int32x8
		nybble = nybble.SetLo(low).SetHi(high).Max(negativeEight).Min(seven)

		restored := prediction.Add(nybble.Mul(delta)).Max(minSample).Min(maxSample)
		index := nybble.Abs().Sub(three).Max(zero).AsUint32x8()
		factor := factorLUT.Permute(index)
		delta = truncDiv256SIMD(delta.Mul(factor)).Max(sixteen)
		errorSum = errorSum.Add(target.Sub(restored).Abs())
		s2 = s1
		s1 = restored
	}

	var sums [8]int32
	errorSum.StoreSlice(sums[:])
	best := 0
	for predictor := 1; predictor < len(coefficients); predictor++ {
		if sums[predictor] < sums[best] {
			best = predictor
		}
	}
	return best
}

func truncDiv256SIMD(value archsimd.Int32x8) archsimd.Int32x8 {
	correction := value.ShiftAllRight(31).And(archsimd.BroadcastInt32x8(255))
	return value.Add(correction).ShiftAllRight(8)
}
