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

func writeSubframeCandidate(w *bits.Writer, samples []int64, bitsPerSample int, best subframeCandidate) error {
	if len(samples) == 0 {
		return fmt.Errorf("FLAC subframe has no samples")
	}
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
	// Shifting out every shared trailing zero bit never costs more than
	// keeping some: the residual magnitudes shrink while the header grows by
	// one unary bit per wasted bit, so a single pass at the full count is
	// sufficient.
	wasted := 0
	if options.enableWastedBits {
		wasted = commonTrailingZeros(samples, bitsPerSample)
	}
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
	}
	return candidate
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
	for order, coefficients := range lpcCoefficientSets(samples, maxLPC) {
		if coefficients == nil {
			continue
		}
		quantized, shift, ok := quantizeLPCCoefficients(coefficients, lpcPrecision)
		if !ok {
			continue
		}
		residual := lpcResidual(samples, order, quantized, shift)
		rice, ok := chooseRiceCodingForBlock(residual, len(samples), order, options.maxRicePartitionOrder)
		if !ok {
			continue
		}
		candidate := subframeCandidate{kind: subframeKindLPC, order: order, residual: residual, rice: rice,
			coeff: quantized, precision: lpcPrecision, shift: shift,
			costBits: uint64(8+order*bitsPerSample+4+5+order*lpcPrecision) + rice.costBits, valid: true}
		if candidate.costBits < best.costBits {
			best = candidate
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

// lpcPrecision is the quantized coefficient precision in bits (including the
// sign bit). The header stores precision-1 in four bits, so 15 is the largest
// legal value.
const lpcPrecision = 15

// lpcCoefficientSets runs a single Levinson-Durbin recursion and returns the
// predictor coefficients for every order in 1..maxOrder (indexed by order;
// index 0 is unused). Orders past a numerically unstable step are nil.
func lpcCoefficientSets(samples []int64, maxOrder int) [][]float64 {
	if maxOrder >= len(samples) {
		maxOrder = len(samples) - 1
	}
	if maxOrder <= 0 {
		return nil
	}
	values := make([]float64, len(samples))
	for i, sample := range samples {
		values[i] = float64(sample)
	}
	auto := make([]float64, maxOrder+1)
	for lag := 0; lag <= maxOrder; lag++ {
		var sum float64
		for i := lag; i < len(values); i++ {
			sum += values[i] * values[i-lag]
		}
		auto[lag] = sum
	}
	if auto[0] == 0 {
		return nil
	}
	sets := make([][]float64, maxOrder+1)
	coeff := make([]float64, maxOrder)
	errorValue := auto[0]
	for i := 0; i < maxOrder; i++ {
		reflection := auto[i+1]
		for j := 0; j < i; j++ {
			reflection -= coeff[j] * auto[i-j]
		}
		if errorValue <= 0 {
			break
		}
		reflection /= errorValue
		if reflection <= -0.999999 || reflection >= 0.999999 || math.IsNaN(reflection) {
			break
		}
		for j := 0; j < i/2; j++ {
			front, back := coeff[j], coeff[i-1-j]
			coeff[j] = front - reflection*back
			coeff[i-1-j] = back - reflection*front
		}
		if i%2 == 1 {
			coeff[i/2] -= reflection * coeff[i/2]
		}
		coeff[i] = reflection
		errorValue *= 1 - reflection*reflection
		sets[i+1] = append([]float64(nil), coeff[:i+1]...)
	}
	return sets
}

// quantizeLPCCoefficients picks the largest shift that keeps every scaled
// coefficient within the precision, then quantizes with error feedback so
// rounding errors do not accumulate across coefficients.
func quantizeLPCCoefficients(coefficients []float64, precision int) ([]int64, int, bool) {
	var max_coeff float64
	for _, coefficient := range coefficients {
		if magnitude := math.Abs(coefficient); magnitude > max_coeff {
			max_coeff = magnitude
		}
	}
	if max_coeff <= 0 || math.IsInf(max_coeff, 0) || math.IsNaN(max_coeff) {
		return nil, 0, false
	}
	_, exponent := math.Frexp(max_coeff)
	shift := precision - 1 - exponent
	// The prediction right shift is stored as a signed 5-bit number that MUST
	// NOT be negative (RFC 9639 Section 9.2.6), so only 0..15 is encodable.
	if shift > 15 {
		shift = 15
	}
	if shift < 0 {
		shift = 0
	}
	min := -(int64(1) << uint(precision-1))
	max := (int64(1) << uint(precision-1)) - 1
	quantized := make([]int64, len(coefficients))
	scale := math.Ldexp(1, shift)
	carry := 0.0
	for i, coefficient := range coefficients {
		value := coefficient*scale + carry
		rounded := math.Round(value)
		if rounded > float64(max) {
			rounded = float64(max)
		} else if rounded < float64(min) {
			rounded = float64(min)
		}
		carry = value - rounded
		quantized[i] = int64(rounded)
	}
	return quantized, shift, true
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
