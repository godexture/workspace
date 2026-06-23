package msadpcm

import (
	"encoding/binary"
	"fmt"
)

func BytesPerPCMBlock(channels int) int {
	blockAlign := 256 * channels
	var samplesPerBlock int
	if channels == 1 {
		samplesPerBlock = (blockAlign-7)*2 + 2
	} else {
		samplesPerBlock = (blockAlign-14)*1 + 2
	}
	return samplesPerBlock * channels * 2
}

func Encode(linear []byte, channels int, byteOrder binary.ByteOrder) ([]byte, error) {
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("unsupported channel count for MS ADPCM: %d", channels)
	}

	numSamples := len(linear) / 2
	if numSamples == 0 {
		return nil, nil
	}

	blockAlign := 256 * channels
	var samplesPerBlock int
	if channels == 1 {
		samplesPerBlock = (blockAlign-7)*2 + 2
	} else {
		samplesPerBlock = (blockAlign-14)*1 + 2
	}

	blockSize := samplesPerBlock * channels

	numBlocks := (numSamples + blockSize - 1) / blockSize
	out := make([]byte, numBlocks*blockAlign)

	chunkSamples := make([]int16, blockSize)

	outIdx := 0
	for i := 0; i < numSamples; i += blockSize {
		toCopy := blockSize
		if numSamples-i < toCopy {
			toCopy = numSamples - i
		}

		for j := 0; j < toCopy; j++ {
			idx := (i + j) * 2
			chunkSamples[j] = int16(byteOrder.Uint16(linear[idx : idx+2]))
		}
		for j := toCopy; j < len(chunkSamples); j++ {
			chunkSamples[j] = 0
		}

		block := out[outIdx : outIdx+blockAlign]
		if channels == 1 {
			encodeMono(block, samplesPerBlock, chunkSamples)
		} else {
			encodeStereo(block, samplesPerBlock, chunkSamples)
		}

		outIdx += blockAlign
	}
	return out, nil
}

func encodeMono(block []byte, samplesPerBlock int, chunkSamples []int16) {
	predictor := 0
	coeff1 := coeffs[predictor][0]
	coeff2 := coeffs[predictor][1]

	sample2 := chunkSamples[0]
	sample1 := chunkSamples[1]

	delta := int32(abs(int(sample1) - int(sample2)))
	if delta < 16 {
		delta = 16
	}

	block[0] = byte(predictor)
	binary.LittleEndian.PutUint16(block[1:3], uint16(delta))
	binary.LittleEndian.PutUint16(block[3:5], uint16(sample1))
	binary.LittleEndian.PutUint16(block[5:7], uint16(sample2))

	s1 := int32(sample1)
	s2 := int32(sample2)

	blockIdx := 7
	for sIdx := 2; sIdx < samplesPerBlock; sIdx += 2 {
		nybbles := [2]uint8{0, 0}
		for n := 0; n < 2; n++ {
			target := int32(chunkSamples[sIdx+n])
			var nybble uint8
			var restored int32
			nybble, restored, delta = encodeStep(target, coeff1, coeff2, delta, s1, s2)
			s2 = s1
			s1 = restored
			nybbles[n] = nybble
		}
		block[blockIdx] = (nybbles[0] << 4) | nybbles[1]
		blockIdx++
	}

}

func encodeStereo(block []byte, samplesPerBlock int, chunkSamples []int16) {
	predL, predR := 0, 0
	coeff1L, coeff2L := coeffs[predL][0], coeffs[predL][1]
	coeff1R, coeff2R := coeffs[predR][0], coeffs[predR][1]

	sample2L := chunkSamples[0]
	sample2R := chunkSamples[1]
	sample1L := chunkSamples[2]
	sample1R := chunkSamples[3]

	deltaL := int32(abs(int(sample1L) - int(sample2L)))
	if deltaL < 16 {
		deltaL = 16
	}
	deltaR := int32(abs(int(sample1R) - int(sample2R)))
	if deltaR < 16 {
		deltaR = 16
	}

	block[0] = byte(predL)
	block[1] = byte(predR)
	binary.LittleEndian.PutUint16(block[2:4], uint16(deltaL))
	binary.LittleEndian.PutUint16(block[4:6], uint16(deltaR))
	binary.LittleEndian.PutUint16(block[6:8], uint16(sample1L))
	binary.LittleEndian.PutUint16(block[8:10], uint16(sample1R))
	binary.LittleEndian.PutUint16(block[10:12], uint16(sample2L))
	binary.LittleEndian.PutUint16(block[12:14], uint16(sample2R))

	s1L, s2L := int32(sample1L), int32(sample2L)
	s1R, s2R := int32(sample1R), int32(sample2R)

	blockIdx := 14
	for sIdx := 2; sIdx < samplesPerBlock; sIdx++ {
		targetL := int32(chunkSamples[sIdx*2])
		var nybbleL uint8
		var restoredL int32
		nybbleL, restoredL, deltaL = encodeStep(targetL, coeff1L, coeff2L, deltaL, s1L, s2L)
		s2L = s1L
		s1L = restoredL

		targetR := int32(chunkSamples[sIdx*2+1])
		var nybbleR uint8
		var restoredR int32
		nybbleR, restoredR, deltaR = encodeStep(targetR, coeff1R, coeff2R, deltaR, s1R, s2R)
		s2R = s1R
		s1R = restoredR

		block[blockIdx] = (nybbleL << 4) | nybbleR
		blockIdx++
	}

}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func encodeStep(target, coeff1, coeff2, delta, sample1, sample2 int32) (uint8, int32, int32) {
	pred := (sample1*coeff1 + sample2*coeff2) / 256
	diff := target - pred

	var signedNybble int32
	if diff >= 0 {
		signedNybble = (diff + (delta / 2)) / delta
	} else {
		signedNybble = (diff - (delta / 2)) / delta
	}

	if signedNybble < -8 {
		signedNybble = -8
	} else if signedNybble > 7 {
		signedNybble = 7
	}

	nybble := uint8(signedNybble)
	if signedNybble < 0 {
		nybble = uint8(signedNybble + 16)
	}

	restored, delta := decodeStep(nybble, coeff1, coeff2, delta, sample1, sample2)

	return nybble, restored, delta
}
