package encoder

import (
	"fmt"
	"math"
	stdbits "math/bits"
	"sync"

	"github.com/godexture/codec-flac/internal/flac"
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

var residualBufferPool sync.Pool

func EncodeSubframeCandidate(w *bits.Writer, samples []int64, bitsPerSample int, best subframeCandidate) error {
	if len(samples) == 0 {
		return fmt.Errorf("FLAC subframe has no samples")
	}
	if !best.valid {
		return fmt.Errorf("no valid FLAC subframe coding")
	}
	reduced := samples
	if best.wastedBits > 0 {
		reduced = getResidualBuffer(len(samples))
		defer releaseResidualBuffer(reduced)
		for i, sample := range samples {
			reduced[i] = sample >> best.wastedBits
		}
	}
	encodeSubframeHeader(w, subframeTypeCode(best.kind, best.order), best.wastedBits)
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
		if err := EncodeResidual(w, best.residual, best.rice); err != nil {
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
		return EncodeResidual(w, best.residual, best.rice)
	}
	return nil
}

func bestSubframe(samples []int64, bitsPerSample int, options flac.EncoderConfig, windows [][]float64) subframeCandidate {
	wasted := 0
	if options.EnableWastedBits {
		wasted = commonTrailingZeros(samples, bitsPerSample)
	}
	reduced := samples
	if wasted > 0 {
		reduced = getResidualBuffer(len(samples))
		defer releaseResidualBuffer(reduced)
		for i, sample := range samples {
			reduced[i] = sample >> wasted
		}
	}
	candidate := bestSubframeWithoutWasted(reduced, bitsPerSample-wasted, options, windows)
	if candidate.valid {
		candidate.wastedBits = wasted
		candidate.costBits += uint64(wasted)
	}
	return candidate
}

func bestSubframeWithoutWasted(samples []int64, bitsPerSample int, options flac.EncoderConfig, windows [][]float64) subframeCandidate {
	best := subframeCandidate{kind: subframeKindVerbatim, costBits: uint64(8 + len(samples)*bitsPerSample), valid: true}
	if isConstant(samples) {
		best = subframeCandidate{kind: subframeKindConstant, costBits: uint64(8 + bitsPerSample), valid: true}
	}

	maxFixed := options.MaxFixedOrder
	if maxFixed > 4 {
		maxFixed = 4
	}
	if maxFixed >= len(samples) {
		maxFixed = len(samples) - 1
	}
	if options.FixedOrderSearch == flac.OrderSearchExhaustive {
		for order := 0; order <= maxFixed; order++ {
			residual := fixedResidual(samples, order)
			rice, ok := chooseRiceCodingForBlock(residual, len(samples), order, options.MaxRicePartitionOrder, options.RiceCost)
			if !ok {
				releaseResidualBuffer(residual)
				continue
			}
			candidate := subframeCandidate{kind: subframeKindFixed, order: order, residual: residual, rice: rice,
				costBits: uint64(8+order*bitsPerSample) + rice.costBits, valid: true}
			if candidate.costBits < best.costBits {
				releaseSubframeCandidate(&best)
				best = candidate
			} else {
				releaseSubframeCandidate(&candidate)
			}
		}
	} else {
		order, residual := bestFixedOrder(samples, maxFixed)
		rice, ok := chooseRiceCodingForBlock(residual, len(samples), order, options.MaxRicePartitionOrder, options.RiceCost)
		if !ok {
			releaseResidualBuffer(residual)
		} else {
			candidate := subframeCandidate{kind: subframeKindFixed, order: order, residual: residual, rice: rice,
				costBits: uint64(8+order*bitsPerSample) + rice.costBits, valid: true}
			if candidate.costBits < best.costBits {
				releaseSubframeCandidate(&best)
				best = candidate
			} else {
				releaseSubframeCandidate(&candidate)
			}
		}
	}

	maxLPC := options.MaxLPCOrder
	if maxLPC > 32 {
		maxLPC = 32
	}
	if maxLPC >= len(samples) {
		maxLPC = len(samples) - 1
	}
	if len(windows) == 0 {
		windows = [][]float64{nil}
	}
	for _, window := range windows {
		for order, coefficients := range lpcCoefficientSets(samples, maxLPC, options.LPCPrecision, options.LPCOrderSearch, window, bitsPerSample) {
			if coefficients == nil {
				continue
			}
			for _, precision := range lpcPrecisionCandidates(options) {
				quantized, shift, ok := quantizeLPCCoefficients(coefficients, precision)
				if !ok {
					continue
				}
				residual := lpcResidual(samples, order, quantized, shift, bitsPerSample)
				rice, ok := chooseRiceCodingForBlock(residual, len(samples), order, options.MaxRicePartitionOrder, options.RiceCost)
				if !ok {
					releaseResidualBuffer(residual)
					continue
				}
				candidate := subframeCandidate{kind: subframeKindLPC, order: order, residual: residual, rice: rice,
					coeff: quantized, precision: precision, shift: shift,
					costBits: uint64(8+order*bitsPerSample+4+5+order*precision) + rice.costBits, valid: true}
				if candidate.costBits < best.costBits {
					releaseSubframeCandidate(&best)
					best = candidate
				} else {
					releaseSubframeCandidate(&candidate)
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

func encodeSubframeHeader(w *bits.Writer, typeCode uint8, wastedBits int) {
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
			zeros = min(zeros, stdbits.TrailingZeros64(value))
		}
		common = min(common, zeros)
		if common == 0 {
			return 0
		}
	}
	return common
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

func fixedPrediction(samples []int64, index, order int) int64 {
	switch order {
	case 0:
		return 0
	case 1:
		return samples[index-1]
	case 2:
		return 2*samples[index-1] - samples[index-2]
	case 3:
		return 3*samples[index-1] - 3*samples[index-2] + samples[index-3]
	case 4:
		return 4*samples[index-1] - 6*samples[index-2] + 4*samples[index-3] - samples[index-4]
	default:
		return 0
	}
}

func lpcPrecisionCandidates(options flac.EncoderConfig) []int {
	precision := options.LPCPrecision
	if precision == 0 {
		precision = flac.DefaultLPCPrecision
	}
	if !options.EnablePrecisionSearch {
		return []int{precision}
	}
	low := min(5, precision)
	result := make([]int, 0, 16-low)
	for value := low; value <= 15; value++ {
		result = append(result, value)
	}
	return result
}

func bestFixedOrder(samples []int64, maxOrder int) (int, []int64) {
	if maxOrder <= 0 {
		return 0, fixedResidual(samples, 0)
	}
	var sums [5]uint64
	stride := max(1, (len(samples)-maxOrder)/512)
	for i := maxOrder; i < len(samples); i += stride {
		value := samples[i]
		sums[0] += foldResidual(value)
		if maxOrder >= 1 {
			sums[1] += foldResidual(value - samples[i-1])
		}
		if maxOrder >= 2 {
			sums[2] += foldResidual(value - 2*samples[i-1] + samples[i-2])
		}
		if maxOrder >= 3 {
			sums[3] += foldResidual(value - 3*samples[i-1] + 3*samples[i-2] - samples[i-3])
		}
		if maxOrder >= 4 {
			sums[4] += foldResidual(value - 4*samples[i-1] + 6*samples[i-2] - 4*samples[i-3] + samples[i-4])
		}
	}
	bestOrder := 0
	for order := 1; order <= maxOrder; order++ {
		if sums[order] < sums[bestOrder] {
			bestOrder = order
		}
	}
	return bestOrder, fixedResidual(samples, bestOrder)
}

func lpcCoefficientSets(samples []int64, maxOrder, precision int, mode flac.OrderSearchMode, window []float64, bitsPerSample int) [][]float64 {
	exhaustive := mode == flac.OrderSearchExhaustive
	if maxOrder >= len(samples) {
		maxOrder = len(samples) - 1
	}
	if maxOrder <= 0 {
		return nil
	}
	if precision == 0 {
		precision = flac.DefaultLPCPrecision
	}
	if window != nil && len(window) != len(samples) {
		return nil
	}
	// Levinson-Durbin recursion; see standard linear-prediction texts.
	values := make([]float64, len(samples))
	windowSamples(samples, window, values, bitsPerSample)
	auto := make([]float64, maxOrder+1)
	autocorrelate(values, auto)
	if auto[0] == 0 {
		return nil
	}
	sets := make([][]float64, maxOrder+1)
	estimates := make([]float64, maxOrder+1)
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
		order := i + 1
		sets[order] = append([]float64(nil), coeff[:order]...)
		if exhaustive {
			continue
		}
		residualSamples := len(samples) - order
		estimates[order] = float64(residualSamples)*math.Max(0, 0.5*math.Log2(errorValue/float64(residualSamples))) + float64(8+order*precision+4+5)
	}
	if exhaustive {
		return sets
	}
	best := 0
	for order := 1; order <= maxOrder; order++ {
		if estimates[order] == 0 {
			continue
		}
		if best == 0 || estimates[order] < estimates[best] {
			best = order
		}
	}
	if best == 0 {
		return sets
	}
	for order := range sets {
		if order != best {
			sets[order] = nil
		}
	}
	return sets
}

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

func getResidualBuffer(length int) []int64 {
	buffer, _ := residualBufferPool.Get().([]int64)
	if cap(buffer) < length {
		return make([]int64, length)
	}
	return buffer[:length]
}

func releaseSubframeCandidate(candidate *subframeCandidate) {
	if candidate.residual != nil {
		releaseResidualBuffer(candidate.residual)
		candidate.residual = nil
	}
	releaseRiceCoding(&candidate.rice)
}

func releaseResidualBuffer(buffer []int64) {
	clear(buffer)
	residualBufferPool.Put(buffer[:0])
}

func releaseSubframeCandidates(candidates []subframeCandidate) {
	for i := range candidates {
		releaseSubframeCandidate(&candidates[i])
	}
}
