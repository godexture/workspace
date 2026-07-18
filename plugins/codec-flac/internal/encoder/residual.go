package encoder

import (
	"errors"
	"math"
	stdbits "math/bits"
	"sync"

	"github.com/godexture/codec-flac/internal/flac"
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

type partitionStats struct {
	sum uint64
	max uint64
}

type riceWorkspace struct {
	folded    []uint64
	stats     []partitionStats
	sums      []uint64
	candidate []ricePartition
	best      []ricePartition
	levels    [16]int
}

var riceWorkspacePool = sync.Pool{New: func() any { return &riceWorkspace{} }}
var ricePartitionPool sync.Pool

func chooseRiceCoding(residual []int64) (riceCoding, bool) {
	return chooseRiceCodingForBlock(residual, len(residual), 0, 15, flac.RiceCostEstimated)
}

var riceMethods = []struct {
	id        uint8
	paramBits uint8
	maxParam  uint8
}{
	{0, 4, 14},
	{1, 5, 30},
}

func chooseRiceCodingForBlock(residual []int64, blockSize, predictorOrder, maxPartitionOrder int, mode flac.RiceCostMode) (riceCoding, bool) {
	workspace := riceWorkspacePool.Get().(*riceWorkspace)
	coding, ok := chooseRiceCodingWithWorkspace(residual, blockSize, predictorOrder, maxPartitionOrder, mode, workspace)
	if ok {
		partitions, _ := ricePartitionPool.Get().([]ricePartition)
		if cap(partitions) < len(coding.partitions) {
			partitions = make([]ricePartition, len(coding.partitions))
		} else {
			partitions = partitions[:len(coding.partitions)]
		}
		copy(partitions, coding.partitions)
		coding.partitions = partitions
	}
	riceWorkspacePool.Put(workspace)
	return coding, ok
}

func releaseRiceCoding(coding *riceCoding) {
	if coding.partitions == nil {
		return
	}
	clear(coding.partitions)
	ricePartitionPool.Put(coding.partitions[:0])
	coding.partitions = nil
}

func chooseRiceCodingWithWorkspace(residual []int64, blockSize, predictorOrder, maxPartitionOrder int, mode flac.RiceCostMode, workspace *riceWorkspace) (riceCoding, bool) {
	exhaustive := mode == flac.RiceCostExact
	if blockSize <= predictorOrder || len(residual) != blockSize-predictorOrder {
		return riceCoding{}, false
	}
	if maxPartitionOrder < 0 {
		return riceCoding{}, false
	}
	if maxPartitionOrder > 15 {
		maxPartitionOrder = 15
	}
	workspace.folded = resize(workspace.folded, len(residual))
	folded := workspace.folded
	maxFolded, ok := foldResidualBatch(residual, folded)
	if !ok {
		return riceCoding{}, false
	}

	deepest := 0
	for deepest < maxPartitionOrder && blockSize%(1<<(deepest+1)) == 0 && blockSize>>(deepest+1) > predictorOrder {
		deepest++
	}

	kMax := stdbits.Len64(maxFolded)
	if kMax > int(riceMethods[1].maxParam) {
		kMax = int(riceMethods[1].maxParam)
	}

	totalStats := 0
	for order := 0; order <= deepest; order++ {
		workspace.levels[order] = totalStats
		totalStats += 1 << order
	}
	workspace.stats = resize(workspace.stats, totalStats)
	clear(workspace.stats)
	stride := 0
	if exhaustive {
		stride = kMax + 1
		workspace.sums = resize(workspace.sums, totalStats*stride)
		clear(workspace.sums)
	}
	workspace.candidate = resize(workspace.candidate, 1<<deepest)
	workspace.best = resize(workspace.best, 1<<deepest)
	statsAt := func(order, partition int) (*partitionStats, []uint64) {
		index := workspace.levels[order] + partition
		if !exhaustive {
			return &workspace.stats[index], nil
		}
		return &workspace.stats[index], workspace.sums[index*stride : (index+1)*stride]
	}

	deepestPartitions := 1 << deepest
	partitionSamples := blockSize >> deepest
	for partition := 0; partition < deepestPartitions; partition++ {
		start := partition*partitionSamples - predictorOrder
		if start < 0 {
			start = 0
		}
		end := (partition+1)*partitionSamples - predictorOrder
		stats, sums := statsAt(deepest, partition)
		values := folded[start:end]
		if exhaustive {
			for _, value := range values {
				// Rice (1979); RFC 9639 §9.2.7.  The sum is the fast estimate.
				stats.sum += value
				for k := 0; k <= kMax && value>>uint(k) > 0; k++ {
					sums[k] += value >> uint(k)
				}
				if value > stats.max {
					stats.max = value
				}
			}
		} else {
			stats.sum, stats.max = sumMaxUint64(values)
		}
	}
	for order := deepest - 1; order >= 0; order-- {
		for partition := 0; partition < 1<<order; partition++ {
			left, leftSums := statsAt(order+1, 2*partition)
			right, rightSums := statsAt(order+1, 2*partition+1)
			merged, mergedSums := statsAt(order, partition)
			merged.sum = left.sum + right.sum
			if exhaustive {
				for k := range mergedSums {
					mergedSums[k] = leftSums[k] + rightSums[k]
				}
			}
			merged.max = left.max
			if right.max > merged.max {
				merged.max = right.max
			}
		}
	}

	best := riceCoding{costBits: math.MaxUint64}
	for partitionOrder := 0; partitionOrder <= deepest; partitionOrder++ {
		partitions := 1 << partitionOrder
		partitionSamples := blockSize >> partitionOrder
		for _, method := range riceMethods {
			if method.id == 1 && kMax <= 14 {
				continue
			}
			chosen := workspace.candidate[:partitions]
			cost := uint64(2 + 4)
			for partition := 0; partition < partitions; partition++ {
				count := partitionSamples
				if partition == 0 {
					count -= predictorOrder
				}
				stats, sums := statsAt(partitionOrder, partition)
				if exhaustive {
					chosen[partition] = bestRicePartition(sums, stats.max, count, method.paramBits, method.maxParam)
				} else {
					chosen[partition] = bestRicePartitionEstimate(stats.sum, stats.max, count, method.paramBits, method.maxParam)
				}
				cost += chosen[partition].costBits
			}
			if cost < best.costBits {
				copy(workspace.best, chosen)
				best = riceCoding{
					method:         method.id,
					paramBits:      method.paramBits,
					partitionOrder: partitionOrder,
					predictorOrder: predictorOrder,
					blockSize:      blockSize,
					partitions:     workspace.best[:partitions],
					costBits:       cost,
				}
			}
		}
	}
	if !exhaustive && best.costBits != math.MaxUint64 {
		best.costBits = exactRiceCodingCost(folded, blockSize, predictorOrder, best.partitionOrder, best.paramBits, best.partitions)
	}
	return best, best.costBits != math.MaxUint64
}

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

func resize[T any](buffer []T, length int) []T {
	if cap(buffer) < length {
		return make([]T, length)
	}
	return buffer[:length]
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

	rawBits := uint8(stdbits.Len64(maxFolded))
	escapeCost := uint64(paramBits) + 5 + uint64(count)*uint64(rawBits)
	if escapeCost < best.costBits {
		best = ricePartition{escaped: true, rawBits: rawBits, costBits: escapeCost}
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
	rawBits := uint8(stdbits.Len64(maxFolded))
	escapeCost := uint64(paramBits) + 5 + uint64(count)*uint64(rawBits)
	if escapeCost < best.costBits {
		best = ricePartition{escaped: true, rawBits: rawBits, costBits: escapeCost}
	}
	return best
}

func EncodeResidual(w *bits.Writer, residual []int64, coding riceCoding) error {
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
	return uint64((value << 1) ^ (value >> 63))
}

func validFLACResidual(value int64) bool {
	return value >= -2147483647 && value <= 2147483647
}
