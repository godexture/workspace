package msadpcm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/codec-pcm/internal/adpcm/bits"
)

func Decode(block []byte, channels int, byteOrder binary.ByteOrder) ([]byte, error) {
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("unsupported channel count for MS ADPCM: %d", channels)
	}

	if channels == 1 {
		if len(block) < 7 {
			return nil, errors.New("MS ADPCM mono block too small")
		}

		return decodeMono(block, byteOrder)
	} else {
		if len(block) < 14 {
			return nil, errors.New("MS ADPCM stereo block too small")
		}

		return decodeStereo(block, byteOrder)
	}
}

func decodeMono(block []byte, byteOrder binary.ByteOrder) ([]byte, error) {
	predictor := int(block[0])
	if predictor > 6 {
		predictor = 0
	}

	delta := int32(int16(binary.LittleEndian.Uint16(block[1:3])))
	sample1 := int32(int16(binary.LittleEndian.Uint16(block[3:5])))
	sample2 := int32(int16(binary.LittleEndian.Uint16(block[5:7])))

	samplesPerBlock := (len(block)-7)*2 + 2
	out := make([]byte, samplesPerBlock*2)

	bits.WriteS16(out, 0, int16(sample2), byteOrder)
	bits.WriteS16(out, 2, int16(sample1), byteOrder)

	coeff1 := coeffs[predictor][0]
	coeff2 := coeffs[predictor][1]

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

func decodeStereo(block []byte, byteOrder binary.ByteOrder) ([]byte, error) {
	predL := int(block[0])
	predR := int(block[1])

	if predL > 6 {
		predL = 0
	}
	if predR > 6 {
		predR = 0
	}

	deltaL := int32(int16(binary.LittleEndian.Uint16(block[2:4])))
	deltaR := int32(int16(binary.LittleEndian.Uint16(block[4:6])))
	sample1L := int32(int16(binary.LittleEndian.Uint16(block[6:8])))
	sample1R := int32(int16(binary.LittleEndian.Uint16(block[8:10])))
	sample2L := int32(int16(binary.LittleEndian.Uint16(block[10:12])))
	sample2R := int32(int16(binary.LittleEndian.Uint16(block[12:14])))

	samplesPerBlock := (len(block)-14)*2 + 4
	out := make([]byte, samplesPerBlock*2)

	bits.WriteS16(out, 0, int16(sample2L), byteOrder)
	bits.WriteS16(out, 2, int16(sample2R), byteOrder)
	bits.WriteS16(out, 4, int16(sample1L), byteOrder)
	bits.WriteS16(out, 6, int16(sample1R), byteOrder)

	coeff1L, coeff2L := coeffs[predL][0], coeffs[predL][1]
	coeff1R, coeff2R := coeffs[predR][0], coeffs[predR][1]

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
