package encoder

import (
	"fmt"

	"github.com/godexture/godec/plugins/codec-flac/internal/config"
	"github.com/godexture/godec/sdk/bits"
	"github.com/godexture/godec/sdk/pool"
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

var residualBufferPool pool.Typed[[]int64]

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

func bestSubframe(samples []int64, bitsPerSample int, options config.EncoderConfig, windows [][]float64, lpc *lpcWorkspace) subframeCandidate {
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
	candidate := bestSubframeWithoutWasted(reduced, bitsPerSample-wasted, options, windows, lpc)
	if candidate.valid {
		candidate.wastedBits = wasted
		candidate.costBits += uint64(wasted)
	}
	return candidate
}

func bestSubframeWithoutWasted(samples []int64, bitsPerSample int, options config.EncoderConfig, windows [][]float64, lpc *lpcWorkspace) subframeCandidate {
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
	if options.FixedOrderSearch == config.OrderSearchExhaustive {
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
		for order, coefficients := range lpcCoefficientSets(samples, maxLPC, options.LPCPrecision, options.LPCOrderSearch, window, bitsPerSample, lpc) {
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
