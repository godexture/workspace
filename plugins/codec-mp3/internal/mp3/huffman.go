package mp3

// BitStreamReader is the bit stream reader state.
type BitStreamReader struct {
	buffer      []byte
	bitPosition int32
	bitLimit    int32
}

// GranuleInfo matches the layout of L3 granule information.
type GranuleInfo struct {
	scaleFactorBandTable          []byte
	part23Length                  uint16
	bigValues                     uint16
	scaleFactorCompression          uint16
	globalGain                      uint8
	blockType                       uint8
	mixedBlockFlag                  uint8
	numberOfLongScaleFactorBands    uint8
	numberOfShortScaleFactorBands   uint8
	tableSelect                     [3]uint8
	regionCount                     [3]uint8
	subblockGain                    [3]uint8
	preemphasisFlag                 uint8
	scaleFactorScale                uint8
	count1Table                     uint8
	scfsi                           uint8
}

func power43Layer3(x int) float32 {
	var frac float32
	var sign, mult int = 0, 256

	if x < 129 {
		return pow43[16+x]
	}

	if x < 1024 {
		mult = 16
		x <<= 3
	}

	sign = (2 * x) & 64
	frac = float32((x&63)-sign) / float32((x&^63)+sign)
	return pow43[16+((x+sign)>>6)] * (1.0 + frac*(4.0/3.0+frac*(2.0/9.0))) * float32(mult)
}

// huffmanDecodeLayer3 performs Huffman decoding for a Layer 3 granule.
func huffmanDecodeLayer3(destinationSamples []float32, bitStreamReader *BitStreamReader, granuleInfo *GranuleInfo, scaleFactors []float32, regionLimit int) {
	if len(destinationSamples) == 0 || bitStreamReader == nil || granuleInfo == nil {
		return
	}

	byteIndex := int(bitStreamReader.bitPosition / 8)

	readByte := func(i int) uint32 {
		if i < 0 || i >= len(bitStreamReader.buffer) {
			return 0
		}
		return uint32(bitStreamReader.buffer[i])
	}

	bitStreamCache := (((readByte(byteIndex)*256+readByte(byteIndex+1))*256+readByte(byteIndex+2))*256 + readByte(byteIndex+3)) << (uint32(bitStreamReader.bitPosition) & 7)
	bitStreamShift := int32((bitStreamReader.bitPosition & 7) - 8)
	byteIndex += 4

	peekBits := func(numberOfBits int) uint32 {
		return bitStreamCache >> (32 - numberOfBits)
	}
	flushBits := func(numberOfBits int) {
		bitStreamCache <<= numberOfBits
		bitStreamShift += int32(numberOfBits)
	}
	checkBits := func() {
		for bitStreamShift >= 0 {
			val := readByte(byteIndex)
			byteIndex++
			bitStreamCache |= val << bitStreamShift
			bitStreamShift -= 8
		}
	}
	bitStreamPosition := func() int {
		return byteIndex*8 - 24 + int(bitStreamShift)
	}

	one := float32(0.0)
	ireg := 0
	bigValuesCount := int(granuleInfo.bigValues)
	scaleFactorBandTable := granuleInfo.scaleFactorBandTable
	scaleFactorBandIndex := 0
	destinationIndex := 0
	scaleFactorIndex := 0

	for bigValuesCount > 0 {
		tableNumber := int(granuleInfo.tableSelect[ireg])
		scaleFactorBandCount := int(granuleInfo.regionCount[ireg])
		ireg++
		codebook := tabs[tableIndex[tableNumber]:]
		linearBits := int(linearBits[tableNumber])

		if linearBits > 0 {
			for {
				numberOfPairs := int(scaleFactorBandTable[scaleFactorBandIndex]) / 2
				scaleFactorBandIndex++
				pairsToDecode := min(bigValuesCount, numberOfPairs)
				one = scaleFactors[scaleFactorIndex]
				scaleFactorIndex++
				for pairsToDecode > 0 {
					bitsToRead := 5
					leafValue := int(codebook[peekBits(bitsToRead)])
					for leafValue < 0 {
						flushBits(bitsToRead)
						bitsToRead = leafValue & 7
						leafValue = int(codebook[int(peekBits(bitsToRead))-(leafValue>>3)])
					}
					flushBits(leafValue >> 8)

					for j := 0; j < 2; j++ {
						leastSignificantBits := leafValue & 0x0F
						if leastSignificantBits == 15 {
							leastSignificantBits += int(peekBits(linearBits))
							flushBits(linearBits)
							checkBits()
							val := one * power43Layer3(leastSignificantBits)
							if int32(bitStreamCache) < 0 {
								val = -val
							}
							destinationSamples[destinationIndex] = val
						} else {
							index := 16 + leastSignificantBits - 16*int(bitStreamCache>>31)
							destinationSamples[destinationIndex] = pow43[index] * one
						}
						destinationIndex++
						if leastSignificantBits != 0 {
							flushBits(1)
						}
						leafValue >>= 4
					}
					checkBits()
					pairsToDecode--
				}
				bigValuesCount -= numberOfPairs
				scaleFactorBandCount--
				if bigValuesCount <= 0 || scaleFactorBandCount < 0 {
					break
				}
			}
		} else {
			for {
				numberOfPairs := int(scaleFactorBandTable[scaleFactorBandIndex]) / 2
				scaleFactorBandIndex++
				pairsToDecode := min(bigValuesCount, numberOfPairs)
				one = scaleFactors[scaleFactorIndex]
				scaleFactorIndex++
				for pairsToDecode > 0 {
					bitsToRead := 5
					leafValue := int(codebook[peekBits(bitsToRead)])
					for leafValue < 0 {
						flushBits(bitsToRead)
						bitsToRead = leafValue & 7
						leafValue = int(codebook[int(peekBits(bitsToRead))-(leafValue>>3)])
					}
					flushBits(leafValue >> 8)

					for j := 0; j < 2; j++ {
						leastSignificantBits := leafValue & 0x0F
						index := 16 + leastSignificantBits - 16*int(bitStreamCache>>31)
						destinationSamples[destinationIndex] = pow43[index] * one
						destinationIndex++
						if leastSignificantBits != 0 {
							flushBits(1)
						}
						leafValue >>= 4
					}
					checkBits()
					pairsToDecode--
				}
				bigValuesCount -= numberOfPairs
				scaleFactorBandCount--
				if bigValuesCount <= 0 || scaleFactorBandCount < 0 {
					break
				}
			}
		}
	}

	numberOfPairs := 1 - bigValuesCount
	for {
		var codebookCount1 []byte
		if granuleInfo.count1Table != 0 {
			codebookCount1 = tab33[:]
		} else {
			codebookCount1 = tab32[:]
		}

		leafValue := int(codebookCount1[peekBits(4)])
		if (leafValue & 8) == 0 {
			leafValue = int(codebookCount1[(leafValue>>3)+int((bitStreamCache<<4)>>(32-(leafValue&3)))])
		}
		flushBits(leafValue & 7)
		if bitStreamPosition() > regionLimit {
			break
		}

		reloadScaleFactor := func() bool {
			numberOfPairs--
			if numberOfPairs == 0 {
				val := int(scaleFactorBandTable[scaleFactorBandIndex])
				scaleFactorBandIndex++
				numberOfPairs = val / 2
				if numberOfPairs == 0 {
					return false
				}
				one = scaleFactors[scaleFactorIndex]
				scaleFactorIndex++
			}
			return true
		}

		dequantizeCount1 := func(s int) {
			if (leafValue & (128 >> s)) != 0 {
				val := one
				if int32(bitStreamCache) < 0 {
					val = -one
				}
				destinationSamples[destinationIndex+s] = val
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

		destinationIndex += 4
		checkBits()
	}

	bitStreamReader.bitPosition = int32(regionLimit)
}
func (r *BitStreamReader) getBits(numberOfBits int) uint32 {
	bitOffset := r.bitPosition & 7
	shiftLeft := numberOfBits + int(bitOffset)
	byteIndex := int(r.bitPosition >> 3)
	r.bitPosition += int32(numberOfBits)
	if r.bitPosition > r.bitLimit {
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
