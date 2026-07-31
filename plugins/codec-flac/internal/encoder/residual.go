package encoder

import (
	"errors"

	"github.com/godexture/godec/sdk/bits"
)

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
				w.UnaryBits64(folded, part.param)
			}
		} else {
			w.Bits64(uint64(maxParam+1), coding.paramBits)
			if part.rawBits > 31 {
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
