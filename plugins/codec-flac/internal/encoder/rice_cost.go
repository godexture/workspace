package encoder

import (
	"math"
	stdbits "math/bits"
)

// exactRiceCodingCost recomputes the true bit cost of the winning partitioning
// chosen via bestRicePartitionEstimate's sum>>k approximation, in a single
// O(len(folded)) pass. It is called once per chooseRiceCodingWithWorkspace
// call (after the search loop, not inside it), so downstream comparisons
// (subframe kind, channel assignment, block split) see the same costBits
// that EncodeResidual will actually emit, while the search itself still uses
// the cheap approximation to pick k and the partition order.
func exactRiceCodingCost(folded []uint64, blockSize, predictorOrder, partitionOrder int, paramBits uint8, partitions []ricePartition) uint64 {
	count := 1 << partitionOrder
	partitionSamples := blockSize >> partitionOrder
	cost := uint64(2 + 4)
	index := 0
	for partition := 0; partition < count; partition++ {
		samples := partitionSamples
		if partition == 0 {
			samples -= predictorOrder
		}
		part := &partitions[partition]
		var partCost uint64
		if part.escaped {
			partCost = uint64(paramBits) + 5 + uint64(samples)*uint64(part.rawBits)
		} else {
			var sum uint64
			for _, value := range folded[index : index+samples] {
				sum += value >> part.param
			}
			partCost = uint64(paramBits) + uint64(samples)*uint64(part.param+1) + sum
		}
		part.costBits = partCost
		cost += partCost
		index += samples
	}
	return cost
}

// realPartitionCost computes the true bit cost of a single already-chosen
// partition (param or escape). bestRicePartitionEstimate's sum>>k shortcut
// over-estimates cost more for large partitions than small ones, which
// otherwise biases chooseRiceCodingWithWorkspace's cross-order comparison
// toward needlessly deep partitioning; this keeps that comparison honest.
func realPartitionCost(folded []uint64, start, end int, part *ricePartition, paramBits uint8) uint64 {
	count := end - start
	if part.escaped {
		return uint64(paramBits) + 5 + uint64(count)*uint64(part.rawBits)
	}
	var sum uint64
	for _, v := range folded[start:end] {
		sum += v >> part.param
	}
	return uint64(paramBits) + uint64(count)*uint64(part.param+1) + sum
}

func bestRicePartition(sums []uint64, maxFolded uint64, count int, paramBits, maxParam uint8) ricePartition {
	best := ricePartition{costBits: math.MaxUint64}
	kEnd := int(maxParam)
	if kEnd > len(sums)-1 {
		kEnd = len(sums) - 1
	}
	for k := 0; k <= kEnd; k++ {
		cost := uint64(paramBits) + uint64(count)*uint64(k+1) + sums[k]
		if cost < best.costBits {
			best = ricePartition{param: uint8(k), costBits: cost}
		}
	}

	if rawBits, ok := escapeRawBits(maxFolded); ok {
		escapeCost := uint64(paramBits) + 5 + uint64(count)*uint64(rawBits)
		if escapeCost < best.costBits {
			best = ricePartition{escaped: true, rawBits: rawBits, costBits: escapeCost}
		}
	}
	return best
}

func bestRicePartitionEstimate(sum, maxFolded uint64, count int, paramBits, maxParam uint8) ricePartition {
	best := ricePartition{costBits: math.MaxUint64}
	k0 := stdbits.Len64(sum / uint64(max(1, count)))
	start, end := max(0, k0-2), min(int(maxParam), k0+2)
	for k := start; k <= end; k++ {
		cost := uint64(paramBits) + uint64(count)*uint64(k+1) + (sum >> uint(k))
		if cost < best.costBits {
			best = ricePartition{param: uint8(k), costBits: cost}
		}
	}
	if rawBits, ok := escapeRawBits(maxFolded); ok {
		escapeCost := uint64(paramBits) + 5 + uint64(count)*uint64(rawBits)
		if escapeCost < best.costBits {
			best = ricePartition{escaped: true, rawBits: rawBits, costBits: escapeCost}
		}
	}
	return best
}

// escapeRawBits returns the raw signed width needed to store any folded
// value up to maxFolded, or ok=false if no width fits: the escape partition's
// width field is 5 bits wide (0-31), one bit short of the 32 raw bits a
// maximally negative FLAC residual can require. When that happens escape
// coding simply isn't viable for the partition; Rice coding remains correct
// (if less efficient) for any magnitude via its unbounded unary quotient, so
// the caller's ordinary per-k search stays the fallback.
func escapeRawBits(maxFolded uint64) (uint8, bool) {
	bits := stdbits.Len64(maxFolded)
	if bits > 31 {
		return 0, false
	}
	return uint8(bits), true
}
