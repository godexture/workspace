package internal

import (
	"errors"
	"math"

	"github.com/godexture/sdk/bits"
)

type ricePartition struct {
	escaped  bool
	param    uint8
	rawBits  uint8
	costBits uint64
}

type riceCoding struct {
	method         uint8
	paramBits      uint8
	partitionOrder int
	predictorOrder int
	blockSize      int
	partitions     []ricePartition
	costBits       uint64
}

// chooseRiceCoding retains the original helper's order-0 API. The encoder
// uses chooseRiceCodingForBlock so partition legality includes the predictor
// warm-up samples.
func chooseRiceCoding(residual []int64) (riceCoding, bool) {
	return chooseRiceCodingForBlock(residual, len(residual), 0, 15)
}

var riceMethods = []struct {
	id        uint8
	paramBits uint8
	maxParam  uint8
}{
	{0, 4, 14},
	{1, 5, 30},
}

func chooseRiceCodingForBlock(residual []int64, blockSize, predictorOrder, maxPartitionOrder int) (riceCoding, bool) {
	if blockSize <= predictorOrder || len(residual) != blockSize-predictorOrder {
		return riceCoding{}, false
	}
	if maxPartitionOrder < 0 {
		return riceCoding{}, false
	}
	if maxPartitionOrder > 15 {
		maxPartitionOrder = 15
	}
	folded := make([]uint64, len(residual))
	var maxFolded uint64
	for i, value := range residual {
		if !validFLACResidual(value) {
			return riceCoding{}, false
		}
		folded[i] = foldResidual(value)
		if folded[i] > maxFolded {
			maxFolded = folded[i]
		}
	}

	// Deepest usable partition order. Partition orders nest (2^n | blockSize
	// implies 2^(n-1) | blockSize and partitions only grow shallower), so the
	// usable orders are exactly 0..deepest and the per-partition sums of one
	// level are the pairwise sums of the level below.
	deepest := 0
	for deepest < maxPartitionOrder && blockSize%(1<<(deepest+1)) == 0 && blockSize>>(deepest+1) > predictorOrder {
		deepest++
	}

	// No Rice parameter above the bit length of the largest folded value can
	// win: beyond it every quotient is zero and the cost only grows.
	kMax := int(foldedBitLength(maxFolded))
	if kMax > int(riceMethods[1].maxParam) {
		kMax = int(riceMethods[1].maxParam)
	}

	// Exact Rice cost for parameter k over a partition of n values is
	// paramBits + n*(k+1) + sum(v>>k), so per-partition prefix sums of v>>k
	// (plus the maximum for the escape width) are all we need. Compute them
	// once at the deepest level and merge upward.
	type partitionStats struct {
		sums []uint64
		max  uint64
	}
	levels := make([][]partitionStats, deepest+1)
	deepestPartitions := 1 << deepest
	partitionSamples := blockSize >> deepest
	levels[deepest] = make([]partitionStats, deepestPartitions)
	for partition := 0; partition < deepestPartitions; partition++ {
		start := partition*partitionSamples - predictorOrder
		if start < 0 {
			start = 0
		}
		end := (partition+1)*partitionSamples - predictorOrder
		stats := partitionStats{sums: make([]uint64, kMax+1)}
		for _, value := range folded[start:end] {
			for k := 0; k <= kMax && value>>uint(k) > 0; k++ {
				stats.sums[k] += value >> uint(k)
			}
			if value > stats.max {
				stats.max = value
			}
		}
		levels[deepest][partition] = stats
	}
	for order := deepest - 1; order >= 0; order-- {
		child := levels[order+1]
		merged := make([]partitionStats, 1<<order)
		for partition := range merged {
			left, right := child[2*partition], child[2*partition+1]
			sums := make([]uint64, kMax+1)
			for k := range sums {
				sums[k] = left.sums[k] + right.sums[k]
			}
			max := left.max
			if right.max > max {
				max = right.max
			}
			merged[partition] = partitionStats{sums: sums, max: max}
		}
		levels[order] = merged
	}

	best := riceCoding{costBits: math.MaxUint64}
	for partitionOrder := 0; partitionOrder <= deepest; partitionOrder++ {
		partitions := 1 << partitionOrder
		partitionSamples := blockSize >> partitionOrder
		for _, method := range riceMethods {
			chosen := make([]ricePartition, partitions)
			cost := uint64(2 + 4)
			for partition := 0; partition < partitions; partition++ {
				count := partitionSamples
				if partition == 0 {
					count -= predictorOrder
				}
				stats := levels[partitionOrder][partition]
				chosen[partition] = bestRicePartition(stats.sums, stats.max, count, method.paramBits, method.maxParam)
				cost += chosen[partition].costBits
			}
			if cost < best.costBits {
				best = riceCoding{
					method: method.id, paramBits: method.paramBits,
					partitionOrder: partitionOrder, predictorOrder: predictorOrder,
					blockSize: blockSize, partitions: chosen, costBits: cost,
				}
			}
		}
	}
	return best, best.costBits != math.MaxUint64
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

	rawBits := foldedBitLength(maxFolded)
	escapeCost := uint64(paramBits) + 5 + uint64(count)*uint64(rawBits)
	if escapeCost < best.costBits {
		best = ricePartition{escaped: true, rawBits: rawBits, costBits: escapeCost}
	}
	return best
}

// foldedBitLength returns the minimum signed width that holds every residual
// whose folded (zigzag) value is at most maxFolded: a signed width w covers
// folded values up to 2^w - 1.
func foldedBitLength(maxFolded uint64) uint8 {
	width := uint8(0)
	for maxFolded > 0 {
		width++
		maxFolded >>= 1
	}
	return width
}

func writeResidual(w *bits.Writer, residual []int64, coding riceCoding) error {
	if coding.method != 0 && coding.method != 1 {
		return errors.New("invalid FLAC Rice coding method")
	}
	if coding.paramBits != 4 && coding.paramBits != 5 {
		return errors.New("invalid FLAC Rice parameter width")
	}
	if coding.partitionOrder < 0 || coding.partitionOrder > 15 || coding.blockSize <= coding.predictorOrder {
		return errors.New("invalid FLAC Rice partition configuration")
	}
	partitions := 1 << coding.partitionOrder
	if len(coding.partitions) != partitions || coding.blockSize%partitions != 0 || len(residual) != coding.blockSize-coding.predictorOrder {
		return errors.New("invalid FLAC Rice partition count")
	}

	w.Bits64(uint64(coding.method), 2)
	w.Bits64(uint64(coding.partitionOrder), 4)
	partitionSamples := coding.blockSize / partitions
	index := 0
	for partition := 0; partition < partitions; partition++ {
		count := partitionSamples
		if partition == 0 {
			count -= coding.predictorOrder
		}
		if count < 0 {
			return errors.New("invalid FLAC Rice predictor order")
		}
		part := coding.partitions[partition]
		maxParam := uint8(14)
		if coding.method == 1 {
			maxParam = 30
		}
		if !part.escaped {
			if part.param > maxParam {
				return errors.New("invalid FLAC Rice parameter")
			}
			w.Bits64(uint64(part.param), coding.paramBits)
			for _, value := range residual[index : index+count] {
				if !validFLACResidual(value) {
					return errors.New("FLAC residual is outside encodable range")
				}
				folded := foldResidual(value)
				w.Unary64(folded >> part.param)
				w.Bits64(folded, part.param)
			}
		} else {
			w.Bits64(uint64(maxParam+1), coding.paramBits)
			if part.rawBits > 32 {
				return errors.New("invalid escaped FLAC residual width")
			}
			w.Bits64(uint64(part.rawBits), 5)
			for _, value := range residual[index : index+count] {
				if !validFLACResidual(value) {
					return errors.New("FLAC residual is outside encodable range")
				}
				w.Signed64(value, part.rawBits)
			}
		}
		index += count
	}
	return nil
}

func foldResidual(value int64) uint64 {
	if value < 0 {
		return uint64(-value*2 - 1)
	}
	return uint64(value * 2)
}
