package internal

import (
	"errors"
	"math"

	"github.com/godexture/sdk/bits"
)

type riceCoding struct {
	method    uint8
	paramBits uint8
	param     uint8
	costBits  uint64
}

func chooseRiceCoding(residual []int64) (riceCoding, bool) {
	if len(residual) == 0 {
		return riceCoding{method: 0, paramBits: 4, param: 0, costBits: 6 + 4}, true
	}
	folded := make([]uint64, len(residual))
	for i, value := range residual {
		if !validFLACResidual(value) {
			return riceCoding{}, false
		}
		folded[i] = foldResidual(value)
	}

	best := riceCoding{costBits: math.MaxUint64}
	for _, candidate := range []struct {
		method    uint8
		paramBits uint8
		maxParam  uint8
	}{
		{method: 0, paramBits: 4, maxParam: 14},
		{method: 1, paramBits: 5, maxParam: 30},
	} {
		for param := uint8(0); param <= candidate.maxParam; param++ {
			cost := uint64(2 + 4 + candidate.paramBits) // method + partition order + Rice parameter
			for _, value := range folded {
				cost += (value >> param) + 1 + uint64(param)
				if cost >= best.costBits {
					break
				}
			}
			if cost < best.costBits {
				best = riceCoding{method: candidate.method, paramBits: candidate.paramBits, param: param, costBits: cost}
			}
		}
	}
	return best, true
}

func writeResidual(w *bits.Writer, residual []int64, coding riceCoding) error {
	if coding.method != 0 && coding.method != 1 {
		return errors.New("invalid FLAC Rice coding method")
	}
	maxParam := uint8(14)
	if coding.method == 1 {
		maxParam = 30
	}
	if coding.param > maxParam {
		return errors.New("invalid FLAC Rice parameter")
	}
	if coding.paramBits != 4 && coding.paramBits != 5 {
		return errors.New("invalid FLAC Rice parameter width")
	}
	w.Bits64(uint64(coding.method), 2)
	w.Bits64(0, 4) // partition order 0
	w.Bits64(uint64(coding.param), coding.paramBits)
	for _, value := range residual {
		if !validFLACResidual(value) {
			return errors.New("FLAC residual is outside encodable range")
		}
		folded := foldResidual(value)
		w.Unary64(folded >> coding.param)
		w.Bits64(folded, coding.param)
	}
	return nil
}

func foldResidual(value int64) uint64 {
	if value < 0 {
		return uint64((-value << 1) - 1)
	}
	return uint64(value << 1)
}

func validFLACResidual(value int64) bool {
	return value >= math.MinInt32 && value <= math.MaxInt32
}
