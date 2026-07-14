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
	for i, value := range residual {
		if !validFLACResidual(value) {
			return riceCoding{}, false
		}
		folded[i] = foldResidual(value)
	}

	best := riceCoding{costBits: math.MaxUint64}
	for partitionOrder := 0; partitionOrder <= maxPartitionOrder; partitionOrder++ {
		partitions := 1 << partitionOrder
		if blockSize%partitions != 0 {
			continue
		}
		partitionSamples := blockSize / partitions
		if partitionSamples <= predictorOrder {
			continue
		}

		for _, method := range []struct {
			id        uint8
			paramBits uint8
			maxParam  uint8
		}{
			{0, 4, 14},
			{1, 5, 30},
		} {
			chosen := make([]ricePartition, partitions)
			cost := uint64(2 + 4)
			index := 0
			for partition := 0; partition < partitions; partition++ {
				count := partitionSamples
				if partition == 0 {
					count -= predictorOrder
				}
				part := folded[index : index+count]
				candidate := chooseRicePartition(part, method.paramBits, method.maxParam)
				chosen[partition] = candidate
				cost += candidate.costBits
				index += count
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

func chooseRicePartition(folded []uint64, paramBits, maxParam uint8) ricePartition {
	best := ricePartition{costBits: math.MaxUint64}
	for param := uint8(0); param <= maxParam; param++ {
		cost := uint64(paramBits)
		for _, value := range folded {
			cost += (value >> param) + 1 + uint64(param)
			if cost >= best.costBits {
				break
			}
		}
		if cost < best.costBits {
			best = ricePartition{param: param, costBits: cost}
		}
	}

	rawBits := minimumSignedWidth(folded)
	escapeCost := uint64(paramBits + 5 + uint8(len(folded))*rawBits)
	if escapeCost < best.costBits {
		best = ricePartition{escaped: true, rawBits: rawBits, costBits: escapeCost}
	}
	return best
}

func minimumSignedWidth(folded []uint64) uint8 {
	var minValue, maxValue int64
	if len(folded) == 0 {
		return 0
	}
	for i, value := range folded {
		decoded := unfoldResidual(value)
		if i == 0 || decoded < minValue {
			minValue = decoded
		}
		if i == 0 || decoded > maxValue {
			maxValue = decoded
		}
	}
	for width := uint8(0); width <= 32; width++ {
		if width == 0 {
			if minValue == 0 && maxValue == 0 {
				return 0
			}
			continue
		}
		min := -(int64(1) << (width - 1))
		max := (int64(1) << (width - 1)) - 1
		if minValue >= min && maxValue <= max {
			return width
		}
	}
	return 32
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

func unfoldResidual(value uint64) int64 {
	if value&1 == 0 {
		return int64(value >> 1)
	}
	return -int64((value >> 1) + 1)
}
