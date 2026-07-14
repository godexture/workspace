package internal

import (
	"fmt"
	"math"

	"github.com/godexture/sdk/bits"
)

type subframeKind uint8

const (
	subframeKindConstant subframeKind = iota
	subframeKindVerbatim
	subframeKindFixed
	subframeKindLPC
)

type subframeCandidate struct {
	kind       subframeKind
	order      int
	residual   []int64
	rice       riceCoding
	coeff      []int64
	precision  int
	shift      int
	wastedBits int
	costBits   uint64
	valid      bool
}

func writeBestSubframe(w *bits.Writer, samples []int64, bitsPerSample int, options frameOptions) error {
	if len(samples) == 0 {
		return fmt.Errorf("FLAC subframe has no samples")
	}
	best := bestSubframe(samples, bitsPerSample, options)
	if !best.valid {
		return fmt.Errorf("no valid FLAC subframe coding")
	}
	reduced := samples
	if best.wastedBits > 0 {
		reduced = make([]int64, len(samples))
		for i, sample := range samples {
			reduced[i] = sample >> best.wastedBits
		}
	}
	writeSubframeHeader(w, subframeTypeCode(best.kind, best.order), best.wastedBits)
	switch best.kind {
	case subframeKindConstant:
		w.Signed64(reduced[0], uint8(bitsPerSample-best.wastedBits))
	case subframeKindVerbatim:
		for _, sample := range reduced {
			w.Signed64(sample, uint8(bitsPerSample-best.wastedBits))
		}
	case subframeKindFixed:
		for i := 0; i < best.order; i++ {
			w.Signed64(reduced[i], uint8(bitsPerSample-best.wastedBits))
		}
		if err := writeResidual(w, best.residual, best.rice); err != nil {
			return err
		}
	case subframeKindLPC:
		for i := 0; i < best.order; i++ {
			w.Signed64(reduced[i], uint8(bitsPerSample-best.wastedBits))
		}
		w.Bits64(uint64(best.precision-1), 4)
		w.Bits64(uint64(best.shift), 5)
		for _, coefficient := range best.coeff {
			w.Signed64(coefficient, uint8(best.precision))
		}
		return writeResidual(w, best.residual, best.rice)
	}
	return nil
}

func bestSubframe(samples []int64, bitsPerSample int, options frameOptions) subframeCandidate {
	best := subframeCandidate{costBits: math.MaxUint64}
	maxWasted := 0
	if options.enableWastedBits {
		maxWasted = commonTrailingZeros(samples, bitsPerSample)
	}
	for wasted := 0; wasted <= maxWasted; wasted++ {
		reduced := samples
		if wasted > 0 {
			reduced = make([]int64, len(samples))
			for i, sample := range samples {
				reduced[i] = sample >> wasted
			}
		}
		candidate := bestSubframeWithoutWasted(reduced, bitsPerSample-wasted, options)
		if candidate.valid {
			candidate.wastedBits = wasted
			candidate.costBits += uint64(wasted)
			if candidate.costBits < best.costBits {
				best = candidate
			}
		}
	}
	return best
}

func bestSubframeWithoutWasted(samples []int64, bitsPerSample int, options frameOptions) subframeCandidate {
	best := subframeCandidate{kind: subframeKindVerbatim, costBits: uint64(8 + len(samples)*bitsPerSample), valid: true}
	if isConstant(samples) {
		best = subframeCandidate{kind: subframeKindConstant, costBits: uint64(8 + bitsPerSample), valid: true}
	}

	maxFixed := options.maxFixedOrder
	if maxFixed > 4 {
		maxFixed = 4
	}
	if maxFixed >= len(samples) {
		maxFixed = len(samples) - 1
	}
	for order := 0; order <= maxFixed; order++ {
		residual := fixedResidual(samples, order)
		rice, ok := chooseRiceCodingForBlock(residual, len(samples), order, options.maxRicePartitionOrder)
		if !ok {
			continue
		}
		candidate := subframeCandidate{kind: subframeKindFixed, order: order, residual: residual, rice: rice,
			costBits: uint64(8+order*bitsPerSample) + rice.costBits, valid: true}
		if candidate.costBits < best.costBits {
			best = candidate
		}
	}

	maxLPC := options.maxLPCOrder
	if maxLPC > 32 {
		maxLPC = 32
	}
	if maxLPC >= len(samples) {
		maxLPC = len(samples) - 1
	}
	for order := 1; order <= maxLPC; order++ {
		coefficients := estimateLPCCoefficients(samples, order)
		if len(coefficients) != order {
			continue
		}
		for precision := 1; precision <= 15; precision++ {
			for shift := 0; shift <= 31; shift++ {
				quantized := quantizeLPC(coefficients, precision, shift)
				if quantized == nil {
					continue
				}
				residual := lpcResidual(samples, order, quantized, shift)
				rice, ok := chooseRiceCodingForBlock(residual, len(samples), order, options.maxRicePartitionOrder)
				if !ok {
					continue
				}
				candidate := subframeCandidate{kind: subframeKindLPC, order: order, residual: residual, rice: rice,
					coeff: quantized, precision: precision, shift: shift,
					costBits: uint64(8+order*bitsPerSample+4+5+order*precision) + rice.costBits, valid: true}
				if candidate.costBits < best.costBits {
					best = candidate
				}
			}
		}
	}
	return best
}

func subframeTypeCode(kind subframeKind, order int) uint8 {
	switch kind {
	case subframeKindConstant:
		return 0
	case subframeKindVerbatim:
		return 1
	case subframeKindFixed:
		return uint8(8 + order)
	case subframeKindLPC:
		return uint8(31 + order)
	default:
		return 1
	}
}

func writeSubframeHeader(w *bits.Writer, typeCode uint8, wastedBits int) {
	w.Bits64(0, 1)
	w.Bits64(uint64(typeCode), 6)
	if wastedBits <= 0 {
		w.Bits64(0, 1)
		return
	}
	w.Bits64(1, 1)
	w.Unary64(uint64(wastedBits - 1))
}

func commonTrailingZeros(samples []int64, bitsPerSample int) int {
	if bitsPerSample <= 1 {
		return 0
	}
	common := bitsPerSample - 1
	for _, sample := range samples {
		value := uint64(sample)
		zeros := bitsPerSample - 1
		if value != 0 {
			zeros = minInt(zeros, trailingZeros(value))
		}
		common = minInt(common, zeros)
	}
	return common
}

func trailingZeros(value uint64) int {
	n := 0
	for value&1 == 0 {
		n++
		value >>= 1
	}
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isConstant(samples []int64) bool {
	first := samples[0]
	for _, sample := range samples[1:] {
		if sample != first {
			return false
		}
	}
	return true
}

func fixedResidual(samples []int64, order int) []int64 {
	residual := make([]int64, 0, len(samples)-order)
	for i := order; i < len(samples); i++ {
		residual = append(residual, samples[i]-fixedPrediction(samples, i, order))
	}
	return residual
}

func estimateLPCCoefficients(samples []int64, order int) []float64 {
	if order <= 0 || len(samples) <= order {
		return nil
	}
	auto := make([]float64, order+1)
	for lag := 0; lag <= order; lag++ {
		for i := lag; i < len(samples); i++ {
			auto[lag] += float64(samples[i]) * float64(samples[i-lag])
		}
	}
	if auto[0] == 0 {
		return nil
	}
	coeff := make([]float64, order)
	errorValue := auto[0]
	for i := 0; i < order; i++ {
		reflection := auto[i+1]
		for j := 0; j < i; j++ {
			reflection -= coeff[j] * auto[i-j]
		}
		if errorValue <= 0 {
			return nil
		}
		reflection /= errorValue
		if reflection <= -0.999999 || reflection >= 0.999999 || math.IsNaN(reflection) {
			return nil
		}
		old := append([]float64(nil), coeff...)
		coeff[i] = reflection
		for j := 0; j < i; j++ {
			coeff[j] = old[j] - reflection*old[i-1-j]
		}
		errorValue *= 1 - reflection*reflection
	}
	return coeff
}

func quantizeLPC(coefficients []float64, precision, shift int) []int64 {
	min := -(int64(1) << uint(precision-1))
	max := (int64(1) << uint(precision-1)) - 1
	result := make([]int64, len(coefficients))
	scale := math.Ldexp(1, shift)
	for i, coefficient := range coefficients {
		value := int64(math.Round(coefficient * scale))
		if value < min || value > max {
			return nil
		}
		result[i] = value
	}
	return result
}

func lpcResidual(samples []int64, order int, coefficients []int64, shift int) []int64 {
	result := make([]int64, 0, len(samples)-order)
	for i := order; i < len(samples); i++ {
		var sum int64
		for j, coefficient := range coefficients {
			sum += coefficient * samples[i-j-1]
		}
		prediction := sum >> uint(shift)
		result = append(result, samples[i]-prediction)
	}
	return result
}
