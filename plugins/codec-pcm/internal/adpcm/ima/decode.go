package imaadpcm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/codec-pcm/internal/adpcm/bits"
	"github.com/godexture/format-wav/params"
)

func Decode(block []byte, channels int, params params.ADPCM, byteOrder binary.ByteOrder) ([]byte, error) {
	if len(block) != int(params.BlockAlign) {
		return nil, fmt.Errorf("IMA ADPCM block size mismatch: got %d, want %d", len(block), params.BlockAlign)
	}
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("unsupported channel count for IMA ADPCM: %d", channels)
	}

	if channels == 1 {
		if len(block) < 4 {
			return nil, errors.New("IMA ADPCM mono block too small")
		}

		return decodeMono(block, byteOrder)
	} else {
		if len(block) < 8 {
			return nil, errors.New("IMA ADPCM stereo block too small")
		}

		return decodeStereo(block, byteOrder)
	}
}

func decodeMono(block []byte, byteOrder binary.ByteOrder) ([]byte, error) {
	sample := int32(int16(binary.LittleEndian.Uint16(block[0:2])))
	stepIndex := int32(block[2])
	if stepIndex > 88 {
		stepIndex = 88
	}

	samplesPerBlock := (len(block)-4)*2 + 1
	out := make([]byte, samplesPerBlock*2)
	bits.WriteS16(out, 0, int16(sample), byteOrder)

	outIdx := 2
	for _, b := range block[4:] {
		nybbles := [2]uint8{b & 0x0F, (b >> 4) & 0x0F}
		for _, nybble := range nybbles {
			sample, stepIndex = decodeStep(nybble, stepIndex, sample)
			bits.WriteS16(out, outIdx, int16(sample), byteOrder)
			outIdx += 2
		}
	}
	return out, nil
}

func decodeStereo(block []byte, byteOrder binary.ByteOrder) ([]byte, error) {
	sampleL := int32(int16(binary.LittleEndian.Uint16(block[0:2])))
	stepIndexL := int32(block[2])
	if stepIndexL > 88 {
		stepIndexL = 88
	}

	sampleR := int32(int16(binary.LittleEndian.Uint16(block[4:6])))
	stepIndexR := int32(block[6])
	if stepIndexR > 88 {
		stepIndexR = 88
	}

	samplesPerBlock := ((len(block)-8)/8)*8 + 1
	out := make([]byte, samplesPerBlock*2*2)

	bits.WriteS16(out, 0, int16(sampleL), byteOrder)
	bits.WriteS16(out, 2, int16(sampleR), byteOrder)

	outIdx := 4
	for blockIdx := 8; blockIdx+8 <= len(block); blockIdx += 8 {
		chunkL := block[blockIdx : blockIdx+4]
		chunkR := block[blockIdx+4 : blockIdx+8]

		var decL [8]int16
		var decR [8]int16

		lIdx := 0
		for _, b := range chunkL {
			nybbles := [2]uint8{b & 0x0F, (b >> 4) & 0x0F}
			for _, nybble := range nybbles {
				sampleL, stepIndexL = decodeStep(nybble, stepIndexL, sampleL)
				decL[lIdx] = int16(sampleL)
				lIdx++
			}
		}

		rIdx := 0
		for _, b := range chunkR {
			nybbles := [2]uint8{b & 0x0F, (b >> 4) & 0x0F}
			for _, nybble := range nybbles {
				sampleR, stepIndexR = decodeStep(nybble, stepIndexR, sampleR)
				decR[rIdx] = int16(sampleR)
				rIdx++
			}
		}

		for i := 0; i < 8; i++ {
			bits.WriteS16(out, outIdx, decL[i], byteOrder)
			bits.WriteS16(out, outIdx+2, decR[i], byteOrder)
			outIdx += 4
		}
	}
	return out, nil
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
