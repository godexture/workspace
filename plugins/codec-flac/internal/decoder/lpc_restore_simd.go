//go:build goexperiment.simd && amd64

package decoder

import (
	"simd/archsimd"
	"unsafe"

	"github.com/godexture/sdk/dsp"
)

type lpcSIMDCoefficients struct {
	reversed [32]int64
	vectors  [8]archsimd.Int64x4
}

func prepareLPCSIMDCoefficients(coefficients []int64, order int) lpcSIMDCoefficients {
	var prepared lpcSIMDCoefficients
	for i := 0; i < order; i++ {
		prepared.reversed[i] = coefficients[order-1-i]
	}
	for i := 0; i+4 <= order; i += 4 {
		prepared.vectors[i/4] = archsimd.LoadInt64x4Slice(prepared.reversed[i:])
	}
	return prepared
}

func sumInt64x4(value archsimd.Int64x4) int64 {
	pairs := value.GetLo().Add(value.GetHi())
	return pairs.GetElem(0) + pairs.GetElem(1)
}

// loadInt64x4At requires index >= 0 and index+4 <= len(samples).
func loadInt64x4At(base unsafe.Pointer, index int) archsimd.Int64x4 {
	return archsimd.LoadInt64x4((*[4]int64)(unsafe.Add(base, uintptr(index)*8)))
}

func restoreLPC(samples, coefficients []int64, order, shift, bitsPerSample int, strict bool) error {
	if strict {
		min, max, err := sampleRangeBounds(bitsPerSample)
		if err != nil {
			return err
		}
		return restoreLPCScalar(samples, coefficients, order, shift, min, max, bitsPerSample)
	}
	if dsp.HasAVX2 && bitsPerSample <= 32 && order >= 8 && order < 32 && len(samples) >= 4096 {
		restoreLPCSIMDUnchecked(samples, coefficients, order, shift)
		return nil
	}
	restoreLPCScalarUnchecked(samples, coefficients, order, shift)
	return nil
}

func restoreLPCSIMDUnchecked(samples, coefficients []int64, order, shift int) {
	coefficients = coefficients[:order:order]
	prepared := prepareLPCSIMDCoefficients(coefficients, order)
	base := unsafe.Pointer(unsafe.SliceData(samples))

	for i := order; i < len(samples); i++ {
		var sumVector archsimd.Int64x4
		j := 0
		// i >= order and j+4 <= order keep the history vector in samples.
		for ; j+4 <= order; j += 4 {
			history := loadInt64x4At(base, i-order+j)
			sumVector = sumVector.Add(prepared.vectors[j/4].AsInt32x8().MulEvenWiden(history.AsInt32x8()))
		}
		sum := sumInt64x4(sumVector)
		for ; j < order; j++ {
			sum += prepared.reversed[j] * samples[i-order+j]
		}
		samples[i] += sum >> shift
	}
}
