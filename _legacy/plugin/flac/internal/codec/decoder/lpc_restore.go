package decoder

import (
	"fmt"
	"unsafe"
)

func loadScalarInt64At(base unsafe.Pointer, index int) int64 {
	return *(*int64)(unsafe.Add(base, uintptr(index)*8))
}

func storeScalarInt64At(base unsafe.Pointer, index int, value int64) {
	*(*int64)(unsafe.Add(base, uintptr(index)*8)) = value
}

func restoreLPCScalarUnchecked(samples, coefficients []int64, order, shift int) {
	coefficients = coefficients[:order:order]
	if order == 32 {
		for i := 32; i < len(samples); i++ {
			history := samples[i-32 : i : i]
			samples[i] += lpcPredictionOrder32(history, coefficients) >> shift
		}
		return
	}
	base := unsafe.Pointer(unsafe.SliceData(samples))
	// i >= order keeps the current sample and every coefficient-relative history index in range.
	for i := order; i < len(samples); i++ {
		value := loadScalarInt64At(base, i) + (lpcPredictionScalarAt(base, i, coefficients) >> shift)
		storeScalarInt64At(base, i, value)
	}
}

func restoreLPCScalar(samples, coefficients []int64, order, shift int, min, max int64, bitsPerSample int) error {
	coefficients = coefficients[:order:order]
	if order == 32 {
		for i := 32; i < len(samples); i++ {
			history := samples[i-32 : i : i]
			value := samples[i] + (lpcPredictionOrder32(history, coefficients) >> shift)
			if value < min || value > max {
				return lpcRangeError(value, bitsPerSample)
			}
			samples[i] = value
		}
		return nil
	}
	base := unsafe.Pointer(unsafe.SliceData(samples))
	// i >= order keeps the current sample and every coefficient-relative history index in range.
	for i := order; i < len(samples); i++ {
		value := loadScalarInt64At(base, i) + (lpcPredictionScalarAt(base, i, coefficients) >> shift)
		if value < min || value > max {
			return lpcRangeError(value, bitsPerSample)
		}
		storeScalarInt64At(base, i, value)
	}
	return nil
}

func lpcPredictionScalarAt(base unsafe.Pointer, index int, coefficients []int64) int64 {
	var sum int64
	for j, coefficient := range coefficients {
		sum += coefficient * loadScalarInt64At(base, index-1-j)
	}
	return sum
}

func lpcPredictionOrder32(history, coefficients []int64) int64 {
	_ = history[31]
	_ = coefficients[31]
	return coefficients[0]*history[31] +
		coefficients[1]*history[30] +
		coefficients[2]*history[29] +
		coefficients[3]*history[28] +
		coefficients[4]*history[27] +
		coefficients[5]*history[26] +
		coefficients[6]*history[25] +
		coefficients[7]*history[24] +
		coefficients[8]*history[23] +
		coefficients[9]*history[22] +
		coefficients[10]*history[21] +
		coefficients[11]*history[20] +
		coefficients[12]*history[19] +
		coefficients[13]*history[18] +
		coefficients[14]*history[17] +
		coefficients[15]*history[16] +
		coefficients[16]*history[15] +
		coefficients[17]*history[14] +
		coefficients[18]*history[13] +
		coefficients[19]*history[12] +
		coefficients[20]*history[11] +
		coefficients[21]*history[10] +
		coefficients[22]*history[9] +
		coefficients[23]*history[8] +
		coefficients[24]*history[7] +
		coefficients[25]*history[6] +
		coefficients[26]*history[5] +
		coefficients[27]*history[4] +
		coefficients[28]*history[3] +
		coefficients[29]*history[2] +
		coefficients[30]*history[1] +
		coefficients[31]*history[0]
}

func lpcRangeError(value int64, bitsPerSample int) error {
	return fmt.Errorf("invalid FLAC LPC prediction: FLAC subframe sample %d outside %d-bit range", value, bitsPerSample)
}
