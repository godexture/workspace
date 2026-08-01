package msadpcm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/godec/plugin/pcm/internal/adpcm/bits"
	"github.com/godexture/godec/plugin/wave/params"
)

func Decode(block []byte, channels int, params params.ADPCM, byteOrder binary.ByteOrder) ([]byte, error) {
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("unsupported channel count for MS ADPCM: %d", channels)
	}
	if len(block) != int(params.BlockAlign) {
		return nil, fmt.Errorf("MS ADPCM block size mismatch: got %d, want %d", len(block), params.BlockAlign)
	}

	if channels == 1 {
		if len(block) < 7 {
			return nil, errors.New("MS ADPCM mono block too small")
		}

		return decodeMono(block, params.Coefficients, byteOrder)
	} else {
		if len(block) < 14 {
			return nil, errors.New("MS ADPCM stereo block too small")
		}

		return decodeStereo(block, params.Coefficients, byteOrder)
	}
}

func decodeMono(block []byte, coefficients []params.Coefficient, byteOrder binary.ByteOrder) ([]byte, error) {
	predictor := int(block[0])
	if predictor >= len(coefficients) {
		return nil, fmt.Errorf("MS ADPCM predictor index out of range: %d", predictor)
	}

	delta := int32(int16(binary.LittleEndian.Uint16(block[1:3])))
	sample1 := int32(int16(binary.LittleEndian.Uint16(block[3:5])))
	sample2 := int32(int16(binary.LittleEndian.Uint16(block[5:7])))

	samplesPerBlock := (len(block)-7)*2 + 2
	out := make([]byte, samplesPerBlock*2)

	bits.WriteS16(out, 0, int16(sample2), byteOrder)
	bits.WriteS16(out, 2, int16(sample1), byteOrder)

	coeff1 := int32(coefficients[predictor].Coeff1)
	coeff2 := int32(coefficients[predictor].Coeff2)

	outIdx := 4
	for _, b := range block[7:] {
		nybbles := [2]uint8{(b >> 4) & 0x0F, b & 0x0F}
		for _, nybble := range nybbles {
			var sample int32
			sample, delta = decodeStep(nybble, coeff1, coeff2, delta, sample1, sample2)
			bits.WriteS16(out, outIdx, int16(sample), byteOrder)
			outIdx += 2

			sample2 = sample1
			sample1 = sample
		}
	}

	return out, nil
}

func decodeStereo(block []byte, coefficients []params.Coefficient, byteOrder binary.ByteOrder) ([]byte, error) {
	predL := int(block[0])
	predR := int(block[1])

	if predL >= len(coefficients) || predR >= len(coefficients) {
		return nil, fmt.Errorf("MS ADPCM predictor index out of range: %d, %d", predL, predR)
	}

	deltaL := int32(int16(binary.LittleEndian.Uint16(block[2:4])))
	deltaR := int32(int16(binary.LittleEndian.Uint16(block[4:6])))
	sample1L := int32(int16(binary.LittleEndian.Uint16(block[6:8])))
	sample1R := int32(int16(binary.LittleEndian.Uint16(block[8:10])))
	sample2L := int32(int16(binary.LittleEndian.Uint16(block[10:12])))
	sample2R := int32(int16(binary.LittleEndian.Uint16(block[12:14])))

	totalSamples := (len(block)-14)*2 + 4
	out := make([]byte, totalSamples*2)

	bits.WriteS16(out, 0, int16(sample2L), byteOrder)
	bits.WriteS16(out, 2, int16(sample2R), byteOrder)
	bits.WriteS16(out, 4, int16(sample1L), byteOrder)
	bits.WriteS16(out, 6, int16(sample1R), byteOrder)

	coeff1L, coeff2L := int32(coefficients[predL].Coeff1), int32(coefficients[predL].Coeff2)
	coeff1R, coeff2R := int32(coefficients[predR].Coeff1), int32(coefficients[predR].Coeff2)

	outIdx := 8
	for _, b := range block[14:] {
		nybbleL := (b >> 4) & 0x0F
		nybbleR := b & 0x0F

		var sampleL int32
		sampleL, deltaL = decodeStep(nybbleL, coeff1L, coeff2L, deltaL, sample1L, sample2L)

		var sampleR int32
		sampleR, deltaR = decodeStep(nybbleR, coeff1R, coeff2R, deltaR, sample1R, sample2R)

		bits.WriteS16(out, outIdx, int16(sampleL), byteOrder)
		bits.WriteS16(out, outIdx+2, int16(sampleR), byteOrder)
		outIdx += 4

		sample2L = sample1L
		sample1L = sampleL

		sample2R = sample1R
		sample1R = sampleR
	}

	return out, nil
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
