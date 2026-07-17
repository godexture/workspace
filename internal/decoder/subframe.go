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
		if err := restoreLPC(samples, coefficients, order, shift, min, max, bitsPerSample); err != nil {
			return err
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

func restoreLPC(samples, coefficients []int64, order, shift int, min, max int64, bitsPerSample int) error {
	if order == 32 {
		coefficients = coefficients[:32:32]
		_ = coefficients[31]
		for i := 32; i < len(samples); i++ {
			history := samples[i-32 : i : i]
			_ = history[31]
			sum :=
				coefficients[0]*history[31] +
					coefficients[1]*history[30] +
					coefficients[2]*history[29] +
					coefficients[3]*history[28] +
					coefficients[4]*history[27] +
					coefficients[5]*history[26] +
					coefficients[6]*history[25] +
					coefficients[7]*history[24] +
					coefficients[8]*history[23] +
					coefficients[9]*history[22] +
					coefficients[10]*history[21] +
					coefficients[11]*history[20] +
					coefficients[12]*history[19] +
					coefficients[13]*history[18] +
					coefficients[14]*history[17] +
					coefficients[15]*history[16] +
					coefficients[16]*history[15] +
					coefficients[17]*history[14] +
					coefficients[18]*history[13] +
					coefficients[19]*history[12] +
					coefficients[20]*history[11] +
					coefficients[21]*history[10] +
					coefficients[22]*history[9] +
					coefficients[23]*history[8] +
					coefficients[24]*history[7] +
					coefficients[25]*history[6] +
					coefficients[26]*history[5] +
					coefficients[27]*history[4] +
					coefficients[28]*history[3] +
					coefficients[29]*history[2] +
					coefficients[30]*history[1] +
					coefficients[31]*history[0]
			value := (sum >> shift) + samples[i]
			if value < min || value > max {
				return fmt.Errorf("invalid FLAC LPC prediction: FLAC subframe sample %d outside %d-bit range", value, bitsPerSample)
			}
			samples[i] = value
		}
		return nil
	}
	for i := order; i < len(samples); i++ {
		var sum int64
		history := samples[i-order : i]
		for j, coefficient := range coefficients {
			sum += coefficient * history[order-1-j]
		}
		value := (sum >> shift) + samples[i]
		if value < min || value > max {
			return fmt.Errorf("invalid FLAC LPC prediction: FLAC subframe sample %d outside %d-bit range", value, bitsPerSample)
		}
		samples[i] = value
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
