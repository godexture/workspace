package decoder

import (
	"errors"
	"fmt"

	"github.com/godexture/sdk/bits"
)

func DecodeSubframe(r *bits.Reader, samples []int64, bitsPerSample int) error {
	blockSize := len(samples)
	originalBitsPerSample := bitsPerSample
	zero, err := r.ReadBits64(1)
	if err != nil {
		return err
	}
	if zero != 0 {
		return errors.New("invalid FLAC subframe header")
	}
	typeCode, err := r.ReadBits64(6)
	if err != nil {
		return err
	}
	wastedFlag, err := r.ReadBits64(1)
	if err != nil {
		return err
	}

	wastedBits := uint64(0)
	if wastedFlag != 0 {
		wastedBits, err = r.ReadUnary64()
		if err != nil {
			return err
		}
		wastedBits++
		if int(wastedBits) >= bitsPerSample {
			return errors.New("invalid FLAC wasted-bits count")
		}
		bitsPerSample -= int(wastedBits)
	}

	switch {
	case typeCode == 0:
		value, err := r.ReadSigned64(uint8(bitsPerSample))
		if err != nil {
			return err
		}
		for i := range samples {
			samples[i] = value
		}

	case typeCode == 1:
		for i := range samples {
			samples[i] = r.Signed64(uint8(bitsPerSample))
		}

	case typeCode >= 8 && typeCode <= 12:
		order := int(typeCode - 8)
		if err := readWarmupSamples(r, samples, order, bitsPerSample); err != nil {
			return err
		}
		if err := DecodeResidualInto(r, samples[order:], blockSize, order); err != nil {
			return err
		}
		min, max, err := sampleRangeBounds(bitsPerSample)
		if err != nil {
			return err
		}
		for i := order; i < blockSize; i++ {
			prediction, err := fixedPredictionChecked(samples, i, order)
			if err != nil {
				return err
			}
			value := prediction + samples[i]
			if value < min || value > max {
				return fmt.Errorf("invalid FLAC fixed prediction: FLAC subframe sample %d outside %d-bit range", value, bitsPerSample)
			}
			samples[i] = value
		}

	case typeCode >= 32 && typeCode <= 63:
		order := int(typeCode - 31)
		if order > blockSize {
			return errors.New("FLAC LPC order exceeds block size")
		}
		if err := readWarmupSamples(r, samples, order, bitsPerSample); err != nil {
			return err
		}
		precisionRaw, err := r.ReadBits64(4)
		if err != nil {
			return err
		}
		if precisionRaw == 15 {
			return errors.New("invalid FLAC LPC coefficient precision")
		}
		precision := int(precisionRaw) + 1
		shiftRaw, err := r.ReadBits64(5)
		if err != nil {
			return err
		}
		shift := signExtend(shiftRaw, 5)
		if shift < 0 {
			return errors.New("negative FLAC LPC shift is reserved")
		}
		coefficients := make([]int64, order)
		for i := range coefficients {
			coeff, err := r.ReadSigned64(uint8(precision))
			if err != nil {
				return err
			}
			coefficients[i] = coeff
		}
		if err := DecodeResidualInto(r, samples[order:], blockSize, order); err != nil {
			return err
		}
		min, max, err := sampleRangeBounds(bitsPerSample)
		if err != nil {
			return err
		}
		for i := order; i < blockSize; i++ {
			var sum int64
			history := samples[i-order : i]
			for j, coefficient := range coefficients {
				sum += coefficient * history[order-1-j]
			}
			sum >>= shift
			value := sum + samples[i]
			if value < min || value > max {
				return fmt.Errorf("invalid FLAC LPC prediction: FLAC subframe sample %d outside %d-bit range", value, bitsPerSample)
			}
			samples[i] = value
		}

	default:
		return fmt.Errorf("unsupported FLAC subframe type: %d", typeCode)
	}

	if wastedBits > 0 {
		for i := range samples {
			samples[i] <<= wastedBits
		}
	}
	min, max, err := sampleRangeBounds(originalBitsPerSample)
	if err != nil {
		return err
	}
	for _, sample := range samples {
		if sample < min || sample > max {
			return fmt.Errorf("FLAC subframe sample %d outside %d-bit range", sample, originalBitsPerSample)
		}
	}
	return nil
}

func sampleRangeBounds(bitsPerSample int) (min, max int64, err error) {
	if bitsPerSample <= 0 || bitsPerSample > 33 {
		return 0, 0, fmt.Errorf("unsupported FLAC subframe bit depth: %d", bitsPerSample)
	}
	min = -(int64(1) << uint(bitsPerSample-1))
	max = (int64(1) << uint(bitsPerSample-1)) - 1
	return min, max, nil
}

func readWarmupSamples(r *bits.Reader, samples []int64, order, bitsPerSample int) error {
	if order > len(samples) {
		return errors.New("FLAC predictor order exceeds block size")
	}
	for i := 0; i < order; i++ {
		value, err := r.ReadSigned64(uint8(bitsPerSample))
		if err != nil {
			return err
		}
		samples[i] = value
	}
	return nil
}

func fixedPrediction(samples []int64, index, order int) int64 {
	switch order {
	case 0:
		return 0
	case 1:
		return samples[index-1]
	case 2:
		return 2*samples[index-1] - samples[index-2]
	case 3:
		return 3*samples[index-1] - 3*samples[index-2] + samples[index-3]
	case 4:
		return 4*samples[index-1] - 6*samples[index-2] + 4*samples[index-3] - samples[index-4]
	default:
		return 0
	}
}

func fixedPredictionChecked(samples []int64, index, order int) (int64, error) {
	if index < order || order < 0 || order > 4 {
		return 0, errors.New("invalid FLAC fixed predictor order")
	}
	return fixedPrediction(samples, index, order), nil
}

func Decorrelate(samples [][]int64, assignment uint8) {
	if len(samples) != 2 {
		return
	}
	switch assignment {
	case 8: // left + side
		for i := range samples[0] {
			samples[1][i] = samples[0][i] - samples[1][i]
		}
	case 9: // side + right
		for i := range samples[0] {
			samples[0][i] += samples[1][i]
		}
	case 10: // mid + side
		for i := range samples[0] {
			mid := samples[0][i]<<1 | (samples[1][i] & 1)
			side := samples[1][i]
			samples[0][i] = (mid + side) >> 1
			samples[1][i] = (mid - side) >> 1
		}
	}
}

func signExtend(value uint64, bits uint8) int {
	if bits == 0 {
		return 0
	}
	if value&(uint64(1)<<(bits-1)) != 0 {
		value |= ^uint64(0) << bits
	}
	return int(int64(value))
}
