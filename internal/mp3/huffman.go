package mp3

// BitReader is the bit stream reader state.
type BitReader struct {
	buffer   []byte
	position int32
	limit    int32
}

// GranuleInfo matches the layout of L3 granule information.
type GranuleInfo struct {
	scaleFactorBandTable      []byte
	part23Length              uint16
	bigValues                 uint16
	scaleFactorCompression    uint16
	globalGain                uint8
	blockType                 uint8
	mixedBlockFlag            uint8
	longScaleFactorBandCount  uint8
	shortScaleFactorBandCount uint8
	tableSelect               [3]uint8
	regionCount               [3]uint8
	subBlockGain              [3]uint8
	preEmphasisFlag           uint8
	scaleFactorScale          uint8
	count1Table               uint8
	scaleFactorSelectionInfo  uint8
}

func power43Layer3(value int) float32 {
	var fraction float32
	var signOffset, multiplier int = 0, 256

	if value < 129 {
		return pow43[16+value]
	}

	if value < 1024 {
		multiplier = 16
		value <<= 3
	}

	signOffset = (2 * value) & 64
	fraction = float32((value&63)-signOffset) / float32((value&^63)+signOffset)
	return pow43[16+((value+signOffset)>>6)] * (1.0 + fraction*(4.0/3.0+fraction*(2.0/9.0))) * float32(multiplier)
}

// huffmanDecodeLayer3 performs Huffman decoding for a Layer 3 granule.
func huffmanDecodeLayer3(samples []float32, bitReader *BitReader, granule *GranuleInfo, scaleFactors []float32, regionLimit int) {
	if len(samples) == 0 || bitReader == nil || granule == nil {
		return
	}

	byteIndex := int(bitReader.position / 8)

	readByte := func(i int) uint32 {
		if i < 0 || i >= len(bitReader.buffer) {
			return 0
		}
		return uint32(bitReader.buffer[i])
	}

	bitCache := (((readByte(byteIndex)*256+readByte(byteIndex+1))*256+readByte(byteIndex+2))*256 + readByte(byteIndex+3)) << (uint32(bitReader.position) & 7)
	bitShift := int32((bitReader.position & 7) - 8)
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
			val := readByte(byteIndex)
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
	bigValuePairs := int(granule.bigValues)
	scaleFactorBandTable := granule.scaleFactorBandTable
	scaleFactorBandIndex := 0
	sampleIndex := 0
	scaleFactorIndex := 0

	for bigValuePairs > 0 {
		tableNumber := int(granule.tableSelect[regionIndex])
		scaleFactorBandCount := int(granule.regionCount[regionIndex])
		regionIndex++
		codeBook := tabs[tableIndex[tableNumber]:]
		linearBits := int(linearBits[tableNumber])

		if linearBits > 0 {
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
							decodedValue += int(peekBits(linearBits))
							flushBits(linearBits)
							checkBits()
							val := scaleFactor * power43Layer3(decodedValue)
							if int32(bitCache) < 0 {
								val = -val
							}
							samples[sampleIndex] = val
						} else {
							index := 16 + decodedValue - 16*int(bitCache>>31)
							samples[sampleIndex] = pow43[index] * scaleFactor
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
						index := 16 + decodedValue - 16*int(bitCache>>31)
						samples[sampleIndex] = pow43[index] * scaleFactor
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
		if granule.count1Table != 0 {
			codeBookCount1 = tab33[:]
		} else {
			codeBookCount1 = tab32[:]
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
				remainingPairsInBand = val / 2
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

	bitReader.position = int32(regionLimit)
}
func (r *BitReader) getBits(width int) uint32 {
	bitOffset := r.position & 7
	shiftLeft := width + int(bitOffset)
	byteIndex := int(r.position >> 3)
	r.position += int32(width)
	if r.position > r.limit {
		return 0
	}
	readByte := func(index int) uint32 {
		if index < 0 || index >= len(r.buffer) {
			return 0
		}
		return uint32(r.buffer[index])
	}
	next := readByte(byteIndex) & (255 >> bitOffset)
	byteIndex++
	bitCache := uint32(0)
	for shiftLeft > 8 {
		shiftLeft -= 8
		bitCache |= next << shiftLeft
		next = readByte(byteIndex)
		byteIndex++
	}
	shiftLeft -= 8
	return bitCache | (next >> -shiftLeft)
}
