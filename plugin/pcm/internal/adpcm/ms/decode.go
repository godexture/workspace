package msadpcm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/godec/plugin/pcm/internal/adpcm/param"
)

var ErrBlockSize = errors.New("MS ADPCM block size mismatch")

// Decode expands one block into per-channel planes. The planes are the
// destination a decoded frame already owns, so a block never allocates.
func Decode(planes [][]int16, block []byte, params param.Parameters) error {
	channels := len(planes)
	if err := params.Validate(param.Microsoft, channels); err != nil {
		return err
	}
	if len(block) != int(params.BlockAlign) {
		return fmt.Errorf("%w: got %d, want %d", ErrBlockSize, len(block), params.BlockAlign)
	}
	for _, plane := range planes {
		if len(plane) < int(params.SamplesPerBlock) {
			return fmt.Errorf("%w: plane holds %d of %d samples", ErrBlockSize, len(plane), params.SamplesPerBlock)
		}
	}
	if channels == 1 {
		return decodeMono(planes[0], block, params.Coefficients)
	}
	return decodeStereo(planes, block, params.Coefficients)
}

// predictor reads one channel's coefficient choice. Each block restates it, so
// a decoder can start on any block.
func predictor(index byte, coefficients []param.Coefficient) (int32, int32, error) {
	if int(index) >= len(coefficients) {
		return 0, 0, fmt.Errorf("MS ADPCM predictor index out of range: %d", index)
	}
	return int32(coefficients[index].Coeff1), int32(coefficients[index].Coeff2), nil
}

func decodeMono(plane []int16, block []byte, coefficients []param.Coefficient) error {
	if len(block) < 7 {
		return fmt.Errorf("%w: mono block is too small", ErrBlockSize)
	}
	coeff1, coeff2, err := predictor(block[0], coefficients)
	if err != nil {
		return err
	}
	delta := int32(int16(binary.LittleEndian.Uint16(block[1:3])))
	sample1 := int32(int16(binary.LittleEndian.Uint16(block[3:5])))
	sample2 := int32(int16(binary.LittleEndian.Uint16(block[5:7])))

	plane[0], plane[1] = int16(sample2), int16(sample1)
	index := 2
	for _, value := range block[7:] {
		for _, nybble := range [2]uint8{(value >> 4) & 0x0F, value & 0x0F} {
			var sample int32
			sample, delta = decodeStep(nybble, coeff1, coeff2, delta, sample1, sample2)
			plane[index] = int16(sample)
			index++
			sample2, sample1 = sample1, sample
		}
	}
	return nil
}

func decodeStereo(planes [][]int16, block []byte, coefficients []param.Coefficient) error {
	if len(block) < 14 {
		return fmt.Errorf("%w: stereo block is too small", ErrBlockSize)
	}
	leftCoeff1, leftCoeff2, err := predictor(block[0], coefficients)
	if err != nil {
		return err
	}
	rightCoeff1, rightCoeff2, err := predictor(block[1], coefficients)
	if err != nil {
		return err
	}
	leftDelta := int32(int16(binary.LittleEndian.Uint16(block[2:4])))
	rightDelta := int32(int16(binary.LittleEndian.Uint16(block[4:6])))
	leftSample1 := int32(int16(binary.LittleEndian.Uint16(block[6:8])))
	rightSample1 := int32(int16(binary.LittleEndian.Uint16(block[8:10])))
	leftSample2 := int32(int16(binary.LittleEndian.Uint16(block[10:12])))
	rightSample2 := int32(int16(binary.LittleEndian.Uint16(block[12:14])))

	planes[0][0], planes[1][0] = int16(leftSample2), int16(rightSample2)
	planes[0][1], planes[1][1] = int16(leftSample1), int16(rightSample1)

	index := 2
	for _, value := range block[14:] {
		var left, right int32
		left, leftDelta = decodeStep((value>>4)&0x0F, leftCoeff1, leftCoeff2, leftDelta, leftSample1, leftSample2)
		right, rightDelta = decodeStep(value&0x0F, rightCoeff1, rightCoeff2, rightDelta, rightSample1, rightSample2)
		planes[0][index], planes[1][index] = int16(left), int16(right)
		index++
		leftSample2, leftSample1 = leftSample1, left
		rightSample2, rightSample1 = rightSample1, right
	}
	return nil
}

func decodeStep(nybble uint8, coeff1, coeff2, delta, sample1, sample2 int32) (int32, int32) {
	signedNybble := int32(nybble)
	if nybble >= 8 {
		signedNybble -= 16
	}
	pred := (sample1*coeff1 + sample2*coeff2) / 256
	sample := pred + signedNybble*delta
	if sample < -32768 {
		sample = -32768
	} else if sample > 32767 {
		sample = 32767
	}

	delta = (delta * adaptionTable[nybble]) / 256
	if delta < 16 {
		delta = 16
	}

	return sample, delta
}
