//go:build goexperiment.simd && amd64

package decoder

import (
	"fmt"
	"simd/archsimd"

	"github.com/godexture/sdk/dsp"
)

func restoreLPC(samples, coefficients []int64, order, shift int, min, max int64, bitsPerSample int) error {
	if dsp.HasAVX2 && bitsPerSample <= 32 && order >= 8 && order < 32 && len(samples)-order >= 4 {
		return restoreLPCSIMD(samples, coefficients, order, shift, min, max, bitsPerSample)
	}
	return restoreLPCScalar(samples, coefficients, order, shift, min, max, bitsPerSample)
}

func restoreLPCSIMD(samples, coefficients []int64, order, shift int, min, max int64, bitsPerSample int) error {
	coefficients = coefficients[:order:order]
	var reversed [32]int64
	for i := range coefficients {
		reversed[i] = coefficients[order-1-i]
	}

	for i := order; i < len(samples); i++ {
		var sumVector archsimd.Int64x4
		j := 0
		for ; j+4 <= order; j += 4 {
			coefficient := archsimd.LoadInt64x4Slice(reversed[j:])
			history := archsimd.LoadInt64x4Slice(samples[i-order+j:])
			sumVector = sumVector.Add(coefficient.AsInt32x8().MulEvenWiden(history.AsInt32x8()))
		}
		var lanes [4]int64
		sumVector.StoreSlice(lanes[:])
		sum := lanes[0] + lanes[1] + lanes[2] + lanes[3]
		for ; j < order; j++ {
			sum += reversed[j] * samples[i-order+j]
		}
		value := (sum >> shift) + samples[i]
		if value < min || value > max {
			return fmt.Errorf("invalid FLAC LPC prediction: FLAC subframe sample %d outside %d-bit range", value, bitsPerSample)
		}
		samples[i] = value
	}
	return nil
}
