package imaadpcm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/godec/plugin/pcm/internal/adpcm/param"
)

var ErrBlockSize = errors.New("IMA ADPCM block size mismatch")

// Decode expands one block into per-channel planes. The planes are the
// destination a decoded frame already owns, so a block never allocates.
func Decode(planes [][]int16, block []byte, params param.Parameters) error {
	channels := len(planes)
	if err := params.Validate(param.IMA, channels); err != nil {
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
		return decodeMono(planes[0], block)
	}
	return decodeStereo(planes, block)
}

// header reads one channel's predictor state, which each block restates so a
// decoder can start on any block.
func header(block []byte) (int32, int32) {
	sample := int32(int16(binary.LittleEndian.Uint16(block[0:2])))
	stepIndex := int32(block[2])
	if stepIndex > 88 {
		stepIndex = 88
	}
	return sample, stepIndex
}

func decodeMono(plane []int16, block []byte) error {
	if len(block) < 4 {
		return fmt.Errorf("%w: mono block is too small", ErrBlockSize)
	}
	sample, stepIndex := header(block)
	plane[0] = int16(sample)
	index := 1
	for _, value := range block[4:] {
		for _, nybble := range [2]uint8{value & 0x0F, (value >> 4) & 0x0F} {
			sample, stepIndex = decodeStep(nybble, stepIndex, sample)
			plane[index] = int16(sample)
			index++
		}
	}
	return nil
}

// decodeStereo walks the block in the eight-sample runs IMA interleaves it in:
// four bytes of the left channel, then four of the right.
func decodeStereo(planes [][]int16, block []byte) error {
	if len(block) < 8 {
		return fmt.Errorf("%w: stereo block is too small", ErrBlockSize)
	}
	left, leftIndex := header(block[0:4])
	right, rightIndex := header(block[4:8])
	planes[0][0], planes[1][0] = int16(left), int16(right)

	position := 1
	for offset := 8; offset+8 <= len(block); offset += 8 {
		start := position
		for _, value := range block[offset : offset+4] {
			for _, nybble := range [2]uint8{value & 0x0F, (value >> 4) & 0x0F} {
				left, leftIndex = decodeStep(nybble, leftIndex, left)
				planes[0][position] = int16(left)
				position++
			}
		}
		position = start
		for _, value := range block[offset+4 : offset+8] {
			for _, nybble := range [2]uint8{value & 0x0F, (value >> 4) & 0x0F} {
				right, rightIndex = decodeStep(nybble, rightIndex, right)
				planes[1][position] = int16(right)
				position++
			}
		}
	}
	return nil
}

func decodeStep(nybble uint8, stepIndex int32, sample int32) (int32, int32) {
	step := stepTable[stepIndex]
	diff := step / 8
	if (nybble & 4) != 0 {
		diff += step
	}
	if (nybble & 2) != 0 {
		diff += step / 2
	}
	if (nybble & 1) != 0 {
		diff += step / 4
	}

	if (nybble & 8) != 0 {
		sample -= diff
	} else {
		sample += diff
	}

	if sample < -32768 {
		sample = -32768
	} else if sample > 32767 {
		sample = 32767
	}

	stepIndex += indexTable[nybble]
	if stepIndex < 0 {
		stepIndex = 0
	}
	if stepIndex > 88 {
		stepIndex = 88
	}

	return sample, stepIndex
}
