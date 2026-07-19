package decoder

import (
	"errors"
	"io"

	"github.com/godexture/sdk/bits"
)

// DecodeResidual reads a FLAC residual coding block. Structural validity
// checks here (reserved coding method, partition math, decoded size) are
// FLAC-spec requirements and always return an error. The per-sample reads
// inside each partition are the hot path, so they use the Fast tier (no
// per-call error); a truncated stream in that inner loop surfaces later via
// Reader.Overrun() rather than aborting this function early.
func DecodeResidual(r *bits.Reader, blockSize, predictorOrder int) ([]int64, error) {
	residual := make([]int64, blockSize-predictorOrder)
	if err := DecodeResidualInto(r, residual, blockSize, predictorOrder); err != nil {
		return nil, err
	}
	return residual, nil
}

func DecodeResidualInto(r *bits.Reader, residual []int64, blockSize, predictorOrder int) error {
	method, err := r.ReadBits32(2)
	if err != nil {
		return err
	}
	var paramBits uint8
	var escape uint32
	switch method {
	case 0:
		paramBits = 4
		escape = 15
	case 1:
		paramBits = 5
		escape = 31
	default:
		return errors.New("reserved FLAC residual coding method")
	}
	partitionOrderRaw, err := r.ReadBits32(4)
	if err != nil {
		return err
	}
	partitionOrder := int(partitionOrderRaw)
	partitions := 1 << partitionOrder
	if blockSize <= predictorOrder || blockSize%partitions != 0 {
		return errors.New("FLAC residual partition order does not divide block size")
	}

	residualCount := blockSize - predictorOrder
	if len(residual) != residualCount {
		return errors.New("FLAC residual destination size mismatch")
	}
	index := 0
	partitionSamples := blockSize / partitions
	for partition := 0; partition < partitions; partition++ {
		samplesInPartition := partitionSamples
		if partition == 0 {
			samplesInPartition -= predictorOrder
		}
		if samplesInPartition < 0 {
			return errors.New("FLAC residual partition smaller than predictor order")
		}

		param, err := r.ReadBits32(paramBits)
		if err != nil {
			return err
		}
		if param == escape {
			rawBits, err := r.ReadBits32(5)
			if err != nil {
				return err
			}
			if rawBits > 32 {
				return errors.New("invalid FLAC escaped residual width")
			}
			for i := 0; i < samplesInPartition; i++ {
				value := r.Signed64(uint8(rawBits))
				if r.Overrun() || !validFLACResidual(value) {
					if r.Overrun() {
						return io.ErrUnexpectedEOF
					}
					return errors.New("FLAC residual is outside encodable range")
				}
				residual[index] = value
				index++
			}
			continue
		}

		for i := 0; i < samplesInPartition; i++ {
			value := decodeRiceSigned(r, uint8(param))
			if r.Overrun() {
				return io.ErrUnexpectedEOF
			}
			if !validFLACResidual(value) {
				return errors.New("FLAC residual is outside encodable range")
			}
			residual[index] = value
			index++
		}
	}
	if index != residualCount {
		return errors.New("decoded FLAC residual size mismatch")
	}
	return nil
}

// decodeRiceSigned decodes one Rice-coded residual sample. It is called per
// sample (potentially thousands of times per frame), so it uses the Fast
// tier: a truncated stream here is detected in aggregate via Overrun()
// rather than per call.
func decodeRiceSigned(r *bits.Reader, param uint8) int64 {
	unsigned := r.Rice64(param)
	quotient := unsigned >> param
	if quotient > uint64(0xffffffff)>>param {
		r.Seek(r.Position())
		return 1 << 62
	}
	if unsigned&1 == 0 {
		return int64(unsigned >> 1)
	}
	return -int64((unsigned >> 1) + 1)
}

// validFLACResidual checks if the residual is within the signed one's-complement
// range. FLAC residuals exclude the two's-complement minimum.
func validFLACResidual(value int64) bool {
	return value >= -2147483647 && value <= 2147483647
}
