package imaadpcm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/godexture/godec/plugin/pcm/internal/adpcm/param"
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

// EncodeBlock codes one block from interleaved samples. Each block restates
// the predictor a decoder starts from, but the step index carries across
// blocks, which is what state holds. The caller owns the block and the sample
// buffer, so coding a stream allocates nothing per block.
func EncodeBlock(block []byte, samples []int16, params param.Parameters, channels int, state *EncodeState) error {
	if err := params.Validate(param.IMA, channels); err != nil {
		return err
	}
	if state == nil {
		return errors.New("IMA ADPCM encoding needs its step index state")
	}
	if len(block) != int(params.BlockAlign) {
		return fmt.Errorf("%w: got %d, want %d", ErrBlockSize, len(block), params.BlockAlign)
	}
	perBlock := int(params.SamplesPerBlock)
	if len(samples) != perBlock*channels {
		return fmt.Errorf("%w: block holds %d of %d samples", ErrBlockSize, len(samples), perBlock*channels)
	}
	if channels == 1 {
		if !state.NotFirstBlock && perBlock > 1 {
			state.StepIndexL = guessStepIndex(int32(samples[1]) - int32(samples[0]))
		}
		state.StepIndexL = encodeMono(block, perBlock, samples, state.StepIndexL)
	} else {
		if !state.NotFirstBlock && perBlock > 1 {
			state.StepIndexL = guessStepIndex(int32(samples[2]) - int32(samples[0]))
			state.StepIndexR = guessStepIndex(int32(samples[3]) - int32(samples[1]))
		}
		state.StepIndexL, state.StepIndexR = encodeStereo(block, perBlock, samples, state.StepIndexL, state.StepIndexR)
	}
	state.NotFirstBlock = true
	return nil
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
