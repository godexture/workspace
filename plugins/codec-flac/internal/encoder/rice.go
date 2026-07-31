package encoder

import (
	"math"
	stdbits "math/bits"
	"github.com/godexture/godec/sdk/pool"

	"github.com/godexture/godec/plugins/codec-flac/internal/config"
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

var riceWorkspacePool = pool.Typed[*riceWorkspace]{}
var ricePartitionPool pool.Typed[[]ricePartition]

func init() {
	riceWorkspacePool.Init(func() *riceWorkspace {
		return &riceWorkspace{}
	})
}

func chooseRiceCoding(residual []int64) (riceCoding, bool) {
	return chooseRiceCodingForBlock(residual, len(residual), 0, 15, config.RiceCostEstimated)
}

var riceMethods = []struct {
	id        uint8
	paramBits uint8
	maxParam  uint8
}{
	{0, 4, 14},
	{1, 5, 30},
}

func chooseRiceCodingForBlock(residual []int64, blockSize, predictorOrder, maxPartitionOrder int, mode config.RiceCostMode) (riceCoding, bool) {
	workspace := riceWorkspacePool.Get()
	coding, ok := chooseRiceCodingWithWorkspace(residual, blockSize, predictorOrder, maxPartitionOrder, mode, workspace)
	if ok {
		partitions := ricePartitionPool.Get()
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

func chooseRiceCodingWithWorkspace(residual []int64, blockSize, predictorOrder, maxPartitionOrder int, mode config.RiceCostMode, workspace *riceWorkspace) (riceCoding, bool) {
	exhaustive := mode == config.RiceCostExact
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
					pstart := partition*partitionSamples - predictorOrder
					if pstart < 0 {
						pstart = 0
					}
					pend := (partition+1)*partitionSamples - predictorOrder
					chosen[partition].costBits = realPartitionCost(folded, pstart, pend, &chosen[partition], method.paramBits)
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

func resize[T any](buffer []T, length int) []T {
	if cap(buffer) < length {
		return make([]T, length)
	}
	return buffer[:length]
}
