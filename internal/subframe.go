package internal

import (
	"errors"
	"fmt"

	"github.com/godexture/sdk/bits"
)

func readSubframe(r *bits.Reader, blockSize, bitsPerSample int) ([]int64, error) {
	originalBitsPerSample := bitsPerSample
	zero, err := r.ReadBits64(1)
	if err != nil {
		return nil, err
	}
	if zero != 0 {
		return nil, errors.New("invalid FLAC subframe header")
	}
	typeCode, err := r.ReadBits64(6)
	if err != nil {
		return nil, err
	}
	wastedFlag, err := r.ReadBits64(1)
	if err != nil {
		return nil, err
	}

	wastedBits := uint64(0)
	if wastedFlag != 0 {
		wastedBits, err = r.ReadUnary64()
		if err != nil {
			return nil, err
		}
		wastedBits++
		if int(wastedBits) >= bitsPerSample {
			return nil, errors.New("invalid FLAC wasted-bits count")
		}
		bitsPerSample -= int(wastedBits)
	}

	samples := make([]int64, blockSize)
	switch {
	case typeCode == 0:
		value, err := r.ReadSigned64(uint8(bitsPerSample))
		if err != nil {
			return nil, err
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
			return nil, err
		}
		residual, err := readResidual(r, blockSize, order)
		if err != nil {
			return nil, err
		}
		min, max, err := sampleRangeBounds(bitsPerSample)
		if err != nil {
			return nil, err
		}
		for i := order; i < blockSize; i++ {
			prediction, err := fixedPredictionChecked(samples, i, order)
			if err != nil {
				return nil, err
			}
			value := prediction + residual[i-order]
			if value < min || value > max {
				return nil, fmt.Errorf("invalid FLAC fixed prediction: FLAC subframe sample %d outside %d-bit range", value, bitsPerSample)
			}
			samples[i] = value
		}

	case typeCode >= 32 && typeCode <= 63:
		order := int(typeCode - 31)
		if order > blockSize {
			return nil, errors.New("FLAC LPC order exceeds block size")
		}
		if err := readWarmupSamples(r, samples, order, bitsPerSample); err != nil {
			return nil, err
		}
		precisionRaw, err := r.ReadBits64(4)
		if err != nil {
			return nil, err
		}
		if precisionRaw == 15 {
			return nil, errors.New("invalid FLAC LPC coefficient precision")
		}
		precision := int(precisionRaw) + 1
		shiftRaw, err := r.ReadBits64(5)
		if err != nil {
			return nil, err
		}
		shift := signExtend(shiftRaw, 5)
		if shift < 0 {
			return nil, errors.New("negative FLAC LPC shift is reserved")
		}
		coefficients := make([]int64, order)
		for i := range coefficients {
			coeff, err := r.ReadSigned64(uint8(precision))
			if err != nil {
				return nil, err
			}
			coefficients[i] = coeff
		}
		residual, err := readResidual(r, blockSize, order)
		if err != nil {
			return nil, err
		}
		min, max, err := sampleRangeBounds(bitsPerSample)
		if err != nil {
			return nil, err
		}
		for i := order; i < blockSize; i++ {
			var sum int64
			for j := 0; j < order; j++ {
				sum += int64(coefficients[j]) * int64(samples[i-j-1])
			}
			sum >>= shift
			value := sum + residual[i-order]
			if value < min || value > max {
				return nil, fmt.Errorf("invalid FLAC LPC prediction: FLAC subframe sample %d outside %d-bit range", value, bitsPerSample)
			}
			samples[i] = value
		}

	default:
		return nil, fmt.Errorf("unsupported FLAC subframe type: %d", typeCode)
	}

	if wastedBits > 0 {
		for i := range samples {
			samples[i] <<= wastedBits
		}
	}
	if err := validateSubframeSampleRange(samples, originalBitsPerSample); err != nil {
		return nil, err
	}
	return samples, nil
}

func validateSubframeSampleRange(samples []int64, bitsPerSample int) error {
	min, max, err := sampleRangeBounds(bitsPerSample)
	if err != nil {
		return err
	}
	for _, sample := range samples {
		if sample < min || sample > max {
			return fmt.Errorf("FLAC subframe sample %d outside %d-bit range", sample, bitsPerSample)
		}
	}
	return nil
}

// sampleRangeBounds hoists the bit-depth check and min/max computation out
// of the per-sample prediction loops (fixed/LPC), which used to call
// validateSubframeSampleRange on a freshly sliced single-element slice for
// every decoded sample.
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
	// Samples are at most 32-bit and the largest fixed predictor coefficient
	// is 6, so int64 provides checked headroom for all RFC-valid input.
	return fixedPrediction(samples, index, order), nil
}

func decorrelate(samples [][]int64, assignment uint8) {
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
