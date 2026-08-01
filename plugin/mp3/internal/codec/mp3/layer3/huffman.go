package layer3

import (
	"github.com/godexture/godec/sdk/bits"
)

// linearize calculates power of 4/3 for the given value
func linearize(value int) float32 {
	var fraction float32
	var signOffset, multiplier int = 0, 256

	if value < 129 {
		return linearizeTable[16+value]
	}

	if value < 1024 {
		multiplier = 16
		value <<= 3
	}

	signOffset = (2 * value) & 64
	fraction = float32((value&63)-signOffset) / float32((value&^63)+signOffset)
	return linearizeTable[16+((value+signOffset)>>6)] * (1.0 + fraction*(4.0/3.0+fraction*(2.0/9.0))) * float32(multiplier)
}

// HuffmanDecode performs Huffman decoding for a granule.
func HuffmanDecode(samples []float32, bitReader *bits.Reader, granule *GranuleInfo, scaleFactors []float32, regionLimit int) {
	if len(samples) == 0 || bitReader == nil || granule == nil {
		return
	}

	byteIndex := int(bitReader.Position() / 8)

	bitCache := (((bitReader.ByteAt(byteIndex)<<8+bitReader.ByteAt(byteIndex+1))<<8+bitReader.ByteAt(byteIndex+2))<<8 + bitReader.ByteAt(byteIndex+3)) << (uint32(bitReader.Position()) & 7)
	bitShift := int32((bitReader.Position() & 7) - 8)
	byteIndex += 4

	peekBits := func(width int) uint32 {
		return bitCache >> (32 - width)
	}
	flushBits := func(width int) {
		bitCache <<= width
		bitShift += int32(width)
	}
	checkBits := func() {
		for bitShift >= 0 {
			val := bitReader.ByteAt(byteIndex)
			byteIndex++
			bitCache |= val << bitShift
			bitShift -= 8
		}
	}
	bitPosition := func() int {
		return byteIndex*8 - 24 + int(bitShift)
	}

	scaleFactor := float32(0.0)
	regionIndex := 0
	bigValuePairs := int(granule.BigValues)
	scaleFactorBandTable := granule.ScaleFactorBandTable
	scaleFactorBandIndex := 0
	sampleIndex := 0
	scaleFactorIndex := 0

	for bigValuePairs > 0 {
		tableNumber := int(granule.TableSelect[regionIndex])
		scaleFactorBandCount := int(granule.RegionCount[regionIndex])
		regionIndex++
		codeBook := huffmanTables[huffmanTableOffsets[tableNumber]:]
		linearBitsVal := int(huffmanTablelinearBits[tableNumber])

		if linearBitsVal > 0 {
			for {
				pairsInBand := int(scaleFactorBandTable[scaleFactorBandIndex]) / 2
				scaleFactorBandIndex++
				pairsToDecode := min(bigValuePairs, pairsInBand)
				scaleFactor = scaleFactors[scaleFactorIndex]
				scaleFactorIndex++
				for pairsToDecode > 0 {
					bitsToRead := 5
					leafValue := int(codeBook[peekBits(bitsToRead)])
					for leafValue < 0 {
						flushBits(bitsToRead)
						bitsToRead = leafValue & 7
						leafValue = int(codeBook[int(peekBits(bitsToRead))-(leafValue>>3)])
					}
					flushBits(leafValue >> 8)

					for j := 0; j < 2; j++ {
						decodedValue := leafValue & 0x0F
						if decodedValue == 15 {
							decodedValue += int(peekBits(linearBitsVal))
							flushBits(linearBitsVal)
							checkBits()
							val := scaleFactor * linearize(decodedValue)
							if int32(bitCache) < 0 {
								val = -val
							}
							samples[sampleIndex] = val
						} else {
							index := 16 + decodedValue - int(bitCache>>31)<<4
							samples[sampleIndex] = linearizeTable[index] * scaleFactor
						}
						sampleIndex++
						if decodedValue != 0 {
							flushBits(1)
						}
						leafValue >>= 4
					}
					checkBits()
					pairsToDecode--
				}
				bigValuePairs -= pairsInBand
				scaleFactorBandCount--
				if bigValuePairs <= 0 || scaleFactorBandCount < 0 {
					break
				}
			}
		} else {
			for {
				pairsInBand := int(scaleFactorBandTable[scaleFactorBandIndex]) >> 1
				scaleFactorBandIndex++
				pairsToDecode := min(bigValuePairs, pairsInBand)
				scaleFactor = scaleFactors[scaleFactorIndex]
				scaleFactorIndex++
				for pairsToDecode > 0 {
					bitsToRead := 5
					leafValue := int(codeBook[peekBits(bitsToRead)])
					for leafValue < 0 {
						flushBits(bitsToRead)
						bitsToRead = leafValue & 7
						leafValue = int(codeBook[int(peekBits(bitsToRead))-(leafValue>>3)])
					}
					flushBits(leafValue >> 8)

					for j := 0; j < 2; j++ {
						decodedValue := leafValue & 0x0F
						index := 16 + decodedValue - int(bitCache>>31)<<4
						samples[sampleIndex] = linearizeTable[index] * scaleFactor
						sampleIndex++
						if decodedValue != 0 {
							flushBits(1)
						}
						leafValue >>= 4
					}
					checkBits()
					pairsToDecode--
				}
				bigValuePairs -= pairsInBand
				scaleFactorBandCount--
				if bigValuePairs <= 0 || scaleFactorBandCount < 0 {
					break
				}
			}
		}
	}

	remainingPairsInBand := 1 - bigValuePairs
	for {
		var codeBookCount1 []byte
		if granule.Count1Table != 0 {
			codeBookCount1 = huffmanCount1TableB[:]
		} else {
			codeBookCount1 = huffmanCount1TableA[:]
		}

		leafValue := int(codeBookCount1[peekBits(4)])
		if (leafValue & 8) == 0 {
			leafValue = int(codeBookCount1[(leafValue>>3)+int((bitCache<<4)>>(32-(leafValue&3)))])
		}
		flushBits(leafValue & 7)
		if bitPosition() > regionLimit {
			break
		}

		reloadScaleFactor := func() bool {
			remainingPairsInBand--
			if remainingPairsInBand == 0 {
				val := int(scaleFactorBandTable[scaleFactorBandIndex])
				scaleFactorBandIndex++
				remainingPairsInBand = val >> 1
				if remainingPairsInBand == 0 {
					return false
				}
				scaleFactor = scaleFactors[scaleFactorIndex]
				scaleFactorIndex++
			}
			return true
		}

		dequantizeCount1 := func(sampleOffset int) {
			if (leafValue & (128 >> sampleOffset)) != 0 {
				sampleValue := scaleFactor
				if int32(bitCache) < 0 {
					sampleValue = -scaleFactor
				}
				samples[sampleIndex+sampleOffset] = sampleValue
				flushBits(1)
			}
		}

		if !reloadScaleFactor() {
			break
		}
		dequantizeCount1(0)
		dequantizeCount1(1)
		if !reloadScaleFactor() {
			break
		}
		dequantizeCount1(2)
		dequantizeCount1(3)

		sampleIndex += 4
		checkBits()
	}

	bitReader.Seek(int32(regionLimit))
}
