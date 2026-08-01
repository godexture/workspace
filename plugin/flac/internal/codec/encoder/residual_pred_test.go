package encoder

import (
	"slices"
	"testing"
	"unsafe"
)

func TestLPCResidualScalarMatchesSerial(t *testing.T) {
	for _, length := range []int{33, 64, 65, 4096} {
		samples := make([]int64, length)
		state := uint32(0x12345678)
		for i := range samples {
			state = state*1664525 + 1013904223
			samples[i] = int64(int32(state)) >> 8
		}
		for order := 1; order <= 32 && order < length; order++ {
			coefficients := make([]int64, order)
			for i := range coefficients {
				coefficients[i] = int64((i*7919)%32768 - 16384)
			}
			for _, shift := range []int{0, 7, 15} {
				got := lpcResidualScalar(samples, order, coefficients, shift)
				want := lpcResidualScalarSerial(samples, order, coefficients, shift)
				if !slices.Equal(got, want) {
					t.Fatalf("length %d, order %d, shift %d: residual mismatch", length, order, shift)
				}
				releaseResidualBuffer(got)
				releaseResidualBuffer(want)
			}
		}
	}
}

func lpcResidualScalarSerial(samples []int64, order int, coefficients []int64, shift int) []int64 {
	coefficients = coefficients[:order:order]
	result := getResidualBuffer(len(samples) - order)
	samplesBase := unsafe.Pointer(unsafe.SliceData(samples))
	resultBase := unsafe.Pointer(unsafe.SliceData(result))
	for i := order; i < len(samples); i++ {
		var sum int64
		for j, coefficient := range coefficients {
			sum += coefficient * loadScalarInt64At(samplesBase, i-1-j)
		}
		prediction := sum >> uint(shift)
		storeScalarInt64At(resultBase, i-order, loadScalarInt64At(samplesBase, i)-prediction)
	}
	return result
}
