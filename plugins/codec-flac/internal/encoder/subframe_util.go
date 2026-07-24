package encoder

import (
	stdbits "math/bits"
)

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
