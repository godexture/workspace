package imaadpcm

import (
	"encoding/binary"
	"fmt"
)

func Encode(linear []byte, channels int, byteOrder binary.ByteOrder) ([]byte, error) {
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("unsupported channel count for IMA ADPCM: %d", channels)
	}

	numSamples := len(linear) / 2
	if numSamples == 0 {
		return nil, nil
	}

	blockAlign := 256 * channels
	var samplesPerBlock int
	if channels == 1 {
		samplesPerBlock = (blockAlign-4)*2 + 1
	} else {
		samplesPerBlock = (blockAlign-8)*1 + 1
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

func encodeMono(block []byte, samplesPerBlock int, samples []int16) {
	sample := samples[0]
	stepIndex := int32(0)

	binary.LittleEndian.PutUint16(block[0:2], uint16(sample))
	block[2] = byte(stepIndex)
	block[3] = 0

	blockIdx := 4

	for sIdx := 1; sIdx < samplesPerBlock; sIdx += 2 {
		nybbles := [2]uint8{0, 0}
		for n := 0; n < 2; n++ {
			target := int32(samples[sIdx+n])
			diff := target - int32(sample)

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

			nybbles[n] = nybble

			predDiff := step / 8
			if (nybble & 4) != 0 {
				predDiff += step
			}
			if (nybble & 2) != 0 {
				predDiff += step / 2
			}
			if (nybble & 1) != 0 {
				predDiff += step / 4
			}

			var newSample int32
			if (nybble & 8) != 0 {
				newSample = int32(sample) - predDiff
			} else {
				newSample = int32(sample) + predDiff
			}

			if newSample < -32768 {
				newSample = -32768
			} else if newSample > 32767 {
				newSample = 32767
			}
			sample = int16(newSample)

			stepIndex += indexTable[nybble]
			if stepIndex < 0 {
				stepIndex = 0
			}
			if stepIndex > 88 {
				stepIndex = 88
			}
		}
		block[blockIdx] = nybbles[0] | (nybbles[1] << 4)
		blockIdx++
	}

}

func encodeStereo(block []byte, samplesPerBlock int, chunkSamples []int16) {
	sampleL := chunkSamples[0]
	sampleR := chunkSamples[1]
	stepIndexL := int32(0)
	stepIndexR := int32(0)

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
			diffL := targetL - int32(sampleL)
			nybbleL := uint8(0)
			if diffL < 0 {
				nybbleL |= 8
				diffL = -diffL
			}
			stepL := stepTable[stepIndexL]
			if diffL >= stepL {
				nybbleL |= 4
				diffL -= stepL
			}
			if diffL >= stepL/2 {
				nybbleL |= 2
				diffL -= stepL / 2
			}
			if diffL >= stepL/4 {
				nybbleL |= 1
			}
			nybblesL[j] = nybbleL

			predL := stepL / 8
			if (nybbleL & 4) != 0 {
				predL += stepL
			}
			if (nybbleL & 2) != 0 {
				predL += stepL / 2
			}
			if (nybbleL & 1) != 0 {
				predL += stepL / 4
			}
			var newL int32
			if (nybbleL & 8) != 0 {
				newL = int32(sampleL) - predL
			} else {
				newL = int32(sampleL) + predL
			}
			if newL < -32768 {
				newL = -32768
			} else if newL > 32767 {
				newL = 32767
			}
			sampleL = int16(newL)
			stepIndexL += indexTable[nybbleL]
			if stepIndexL < 0 {
				stepIndexL = 0
			}
			if stepIndexL > 88 {
				stepIndexL = 88
			}

			targetR := int32(chunkSamples[(sIdx+j)*2+1])
			diffR := targetR - int32(sampleR)
			nybbleR := uint8(0)
			if diffR < 0 {
				nybbleR |= 8
				diffR = -diffR
			}
			stepR := stepTable[stepIndexR]
			if diffR >= stepR {
				nybbleR |= 4
				diffR -= stepR
			}
			if diffR >= stepR/2 {
				nybbleR |= 2
				diffR -= stepR / 2
			}
			if diffR >= stepR/4 {
				nybbleR |= 1
			}
			nybblesR[j] = nybbleR

			predR := stepR / 8
			if (nybbleR & 4) != 0 {
				predR += stepR
			}
			if (nybbleR & 2) != 0 {
				predR += stepR / 2
			}
			if (nybbleR & 1) != 0 {
				predR += stepR / 4
			}
			var newR int32
			if (nybbleR & 8) != 0 {
				newR = int32(sampleR) - predR
			} else {
				newR = int32(sampleR) + predR
			}
			if newR < -32768 {
				newR = -32768
			} else if newR > 32767 {
				newR = 32767
			}
			sampleR = int16(newR)
			stepIndexR += indexTable[nybbleR]
			if stepIndexR < 0 {
				stepIndexR = 0
			}
			if stepIndexR > 88 {
				stepIndexR = 88
			}
		}

		for j := 0; j < 4; j++ {
			block[blockIdx+j] = nybblesL[j*2] | (nybblesL[j*2+1] << 4)
		}
		for j := 0; j < 4; j++ {
			block[blockIdx+4+j] = nybblesR[j*2] | (nybblesR[j*2+1] << 4)
		}
		blockIdx += 8
	}

}
