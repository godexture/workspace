package imaadpcm

import (
	"encoding/binary"
	"fmt"

	"github.com/godexture/godec/plugin/wave/params"
)

func BytesPerPCMBlock(channels int, blockAlign int) int {
	samplesPerBlock := (blockAlign-4*channels)*2/channels + 1
	return samplesPerBlock * channels * 2
}

type EncodeState struct {
	StepIndexL    int32
	StepIndexR    int32
	NotFirstBlock bool
}

func Encode(linear []byte, channels int, params params.ADPCM, byteOrder binary.ByteOrder, state *EncodeState) ([]byte, error) {
	if state == nil {
		state = &EncodeState{}
	}
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("unsupported channel count for IMA ADPCM: %d", channels)
	}

	numSamples := len(linear) / 2
	if numSamples == 0 {
		return nil, nil
	}

	blockAlign := int(params.BlockAlign)
	var samplesPerBlock int
	if channels == 1 {
		samplesPerBlock = (blockAlign-4)*2 + 1
	} else {
		samplesPerBlock = (blockAlign-8)*1 + 1
	}
	if samplesPerBlock != int(params.SamplesPerBlock) {
		return nil, fmt.Errorf("IMA ADPCM samples per block mismatch: got %d, want %d", params.SamplesPerBlock, samplesPerBlock)
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
			if !state.NotFirstBlock && samplesPerBlock > 1 {
				diff := int32(chunkSamples[1]) - int32(chunkSamples[0])
				state.StepIndexL = guessStepIndex(diff)
			}
			state.StepIndexL = encodeMono(block, samplesPerBlock, chunkSamples, state.StepIndexL)
		} else {
			if !state.NotFirstBlock && samplesPerBlock > 1 {
				diffL := int32(chunkSamples[2]) - int32(chunkSamples[0])
				diffR := int32(chunkSamples[3]) - int32(chunkSamples[1])
				state.StepIndexL = guessStepIndex(diffL)
				state.StepIndexR = guessStepIndex(diffR)
			}
			state.StepIndexL, state.StepIndexR = encodeStereo(block, samplesPerBlock, chunkSamples, state.StepIndexL, state.StepIndexR)
		}
		state.NotFirstBlock = true

		outIdx += blockAlign
	}

	return out, nil
}

func encodeMono(block []byte, samplesPerBlock int, samples []int16, stepIndex int32) int32 {
	sample := samples[0]

	binary.LittleEndian.PutUint16(block[0:2], uint16(sample))
	block[2] = byte(stepIndex)
	block[3] = 0

	blockIdx := 4

	for sIdx := 1; sIdx < samplesPerBlock; sIdx += 2 {
		nybbles := [2]uint8{0, 0}
		for n := 0; n < 2; n++ {
			target := int32(samples[sIdx+n])
			var nybble uint8
			var newSample int32
			nybble, stepIndex, newSample = encodeStep(target, stepIndex, int32(sample))
			sample = int16(newSample)
			nybbles[n] = nybble
		}
		block[blockIdx] = nybbles[0] | (nybbles[1] << 4)
		blockIdx++
	}
	return stepIndex
}

func encodeStereo(block []byte, samplesPerBlock int, chunkSamples []int16, stepIndexL int32, stepIndexR int32) (int32, int32) {
	sampleL := chunkSamples[0]
	sampleR := chunkSamples[1]

	binary.LittleEndian.PutUint16(block[0:2], uint16(sampleL))
	block[2] = byte(stepIndexL)
	block[3] = 0
	binary.LittleEndian.PutUint16(block[4:6], uint16(sampleR))
	block[6] = byte(stepIndexR)
	block[7] = 0

	blockIdx := 8

	for sIdx := 1; sIdx < samplesPerBlock; sIdx += 8 {
		var nybblesL [8]uint8
		var nybblesR [8]uint8

		for j := 0; j < 8; j++ {
			targetL := int32(chunkSamples[(sIdx+j)*2])
			var nybbleL uint8
			var newL int32
			nybbleL, stepIndexL, newL = encodeStep(targetL, stepIndexL, int32(sampleL))
			sampleL = int16(newL)
			nybblesL[j] = nybbleL

			targetR := int32(chunkSamples[(sIdx+j)*2+1])
			var nybbleR uint8
			var newR int32
			nybbleR, stepIndexR, newR = encodeStep(targetR, stepIndexR, int32(sampleR))
			sampleR = int16(newR)
			nybblesR[j] = nybbleR
		}

		for j := 0; j < 4; j++ {
			block[blockIdx+j] = nybblesL[j*2] | (nybblesL[j*2+1] << 4)
		}
		for j := 0; j < 4; j++ {
			block[blockIdx+4+j] = nybblesR[j*2] | (nybblesR[j*2+1] << 4)
		}
		blockIdx += 8
	}

	return stepIndexL, stepIndexR
}

func encodeStep(target int32, stepIndex int32, sample int32) (uint8, int32, int32) {
	diff := target - sample

	nybble := uint8(0)
	if diff < 0 {
		nybble |= 8
		diff = -diff
	}

	step := stepTable[stepIndex]
	if diff >= step {
		nybble |= 4
		diff -= step
	}
	if diff >= step/2 {
		nybble |= 2
		diff -= step / 2
	}
	if diff >= step/4 {
		nybble |= 1
	}

	sample, stepIndex = decodeStep(nybble, stepIndex, sample)

	return nybble, stepIndex, sample
}

func guessStepIndex(diff int32) int32 {
	if diff < 0 {
		diff = -diff
	}
	idealStep := diff / 2
	if idealStep < 7 {
		return 0
	}
	for i, s := range stepTable {
		if s > idealStep {
			if i > 0 {
				return int32(i - 1)
			}
			return 0
		}
	}
	return 88
}
