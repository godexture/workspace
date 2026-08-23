package msadpcm

import (
	"encoding/binary"
	"fmt"

	"github.com/godexture/godec/plugin/pcm/internal/adpcm/param"
)

func BytesPerPCMBlock(channels int, blockAlign int) int {
	var samplesPerBlock int
	if channels == 1 {
		samplesPerBlock = (blockAlign-7)*2 + 2
	} else {
		samplesPerBlock = (blockAlign-14)*1 + 2
	}
	return samplesPerBlock * channels * 2
}

// EncodeBlock codes one block from interleaved samples. The caller owns the
// block and the sample buffer, so coding a stream allocates nothing per block.
// samples must hold SamplesPerBlock frames; a short final block is the caller
// to pad, because only it knows the stream ended.
func EncodeBlock(block []byte, samples []int16, params param.Parameters, channels int) error {
	if err := params.Validate(param.Microsoft, channels); err != nil {
		return err
	}
	if len(block) != int(params.BlockAlign) {
		return fmt.Errorf("%w: got %d, want %d", ErrBlockSize, len(block), params.BlockAlign)
	}
	if len(samples) != int(params.SamplesPerBlock)*channels {
		return fmt.Errorf("%w: block holds %d of %d samples", ErrBlockSize, len(samples), int(params.SamplesPerBlock)*channels)
	}
	if channels == 1 {
		encodeMono(block, int(params.SamplesPerBlock), samples, params.Coefficients)
	} else {
		encodeStereo(block, int(params.SamplesPerBlock), samples, params.Coefficients)
	}
	return nil
}

func encodeMono(block []byte, samplesPerBlock int, chunkSamples []int16, coefficients []param.Coefficient) {
	predictor := findBestPredictor(chunkSamples, samplesPerBlock, 1, 0, coefficients)
	coeff1 := int32(coefficients[predictor].Coeff1)
	coeff2 := int32(coefficients[predictor].Coeff2)

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

func encodeStereo(block []byte, samplesPerBlock int, chunkSamples []int16, coefficients []param.Coefficient) {
	predL := findBestPredictor(chunkSamples, samplesPerBlock, 2, 0, coefficients)
	predR := findBestPredictor(chunkSamples, samplesPerBlock, 2, 1, coefficients)
	coeff1L, coeff2L := int32(coefficients[predL].Coeff1), int32(coefficients[predL].Coeff2)
	coeff1R, coeff2R := int32(coefficients[predR].Coeff1), int32(coefficients[predR].Coeff2)

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

func findBestPredictorScalar(chunkSamples []int16, samplesPerBlock int, step int, offset int, coefficients []param.Coefficient) int {
	bestPredictor := 0
	var minError int64 = -1

	sample2 := chunkSamples[0*step+offset]
	sample1 := chunkSamples[1*step+offset]
	initialDelta := int32(abs(int(sample1) - int(sample2)))
	if initialDelta < 16 {
		initialDelta = 16
	}

	for p, coefficient := range coefficients {
		coeff1 := int32(coefficient.Coeff1)
		coeff2 := int32(coefficient.Coeff2)

		s1 := int32(sample1)
		s2 := int32(sample2)
		delta := initialDelta
		var totalError int64

		for sIdx := 2; sIdx < samplesPerBlock; sIdx++ {
			target := int32(chunkSamples[sIdx*step+offset])
			_, restored, nextDelta := encodeStep(target, coeff1, coeff2, delta, s1, s2)
			totalError += int64(abs(int(target - restored)))
			s2 = s1
			s1 = restored
			delta = nextDelta
		}

		if minError == -1 || totalError < minError {
			minError = totalError
			bestPredictor = p
		}
	}
	return bestPredictor
}
