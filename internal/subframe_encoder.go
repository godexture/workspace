package internal

import (
	"fmt"

	"github.com/godexture/sdk/bits"
)

type subframeKind uint8

const (
	subframeKindConstant subframeKind = iota
	subframeKindVerbatim
	subframeKindFixed
)

type subframeCandidate struct {
	kind      subframeKind
	order     int
	residual  []int64
	rice      riceCoding
	costBits  uint64
	validRice bool
}

func writeBestSubframe(w *bits.Writer, samples []int64, bitsPerSample, maxFixedOrder int) error {
	if len(samples) == 0 {
		return fmt.Errorf("FLAC subframe has no samples")
	}
	if isConstant(samples) {
		writeSubframeHeader(w, 0)
		w.Signed64(samples[0], uint8(bitsPerSample))
		return nil
	}

	best := subframeCandidate{
		kind:     subframeKindVerbatim,
		costBits: uint64(8 + len(samples)*bitsPerSample),
	}

	if maxFixedOrder > 4 {
		maxFixedOrder = 4
	}
	if maxFixedOrder >= len(samples) {
		maxFixedOrder = len(samples) - 1
	}
	for order := 0; order <= maxFixedOrder; order++ {
		residual := fixedResidual(samples, order)
		rice, ok := chooseRiceCoding(residual)
		if !ok {
			continue
		}
		cost := uint64(8 + order*bitsPerSample)
		cost += rice.costBits
		if cost < best.costBits {
			best = subframeCandidate{
				kind:      subframeKindFixed,
				order:     order,
				residual:  residual,
				rice:      rice,
				costBits:  cost,
				validRice: true,
			}
		}
	}

	switch best.kind {
	case subframeKindFixed:
		writeSubframeHeader(w, uint8(8+best.order))
		for i := 0; i < best.order; i++ {
			w.Signed64(samples[i], uint8(bitsPerSample))
		}
		return writeResidual(w, best.residual, best.rice)
	default:
		writeSubframeHeader(w, 1)
		for _, sample := range samples {
			w.Signed64(sample, uint8(bitsPerSample))
		}
		return nil
	}
}

func writeSubframeHeader(w *bits.Writer, typeCode uint8) {
	w.Bits64(0, 1)
	w.Bits64(uint64(typeCode), 6)
	w.Bits64(0, 1) // wasted-bits flag; MVP does not use wasted-bits coding
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
