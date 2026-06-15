package mp3

type Layer12ScaleFactorInfo struct {
	ScaleFactors                [3 * 64]float32
	TotalBands                  uint8
	StereoBands                 uint8
	BitAllocation               [64]uint8
	ScaleFactorTransmissionCode [64]uint8
}

type Layer12SubbandAllocation struct {
	TableOffset    uint8
	CodeTableWidth uint8
	BandCount      uint8
}

var allocationTableLayer1 = []Layer12SubbandAllocation{{76, 4, 32}}
var allocationTableLayer2MPEG2 = []Layer12SubbandAllocation{{60, 4, 4}, {44, 3, 7}, {44, 2, 19}}
var allocationTableLayer2MPEG1 = []Layer12SubbandAllocation{{0, 4, 3}, {16, 4, 8}, {32, 3, 12}, {40, 2, 7}}
var allocationTableLayer2MPEG1LowRate = []Layer12SubbandAllocation{{44, 4, 2}, {44, 3, 10}}

func subbandAllocationTableLayer12(header Header, scaleFactorInfo *Layer12ScaleFactorInfo) []Layer12SubbandAllocation {
	var allocation []Layer12SubbandAllocation
	mode := header.StereoMode()
	stereoBands := 32
	if mode == 3 { // MODE_MONO
		stereoBands = 0
	} else if mode == 1 { // MODE_JOINT_STEREO
		stereoBands = (header.StereoModeExt() << 2) + 4
	}

	numberOfBands := 0
	if header.IsLayer1() {
		allocation = allocationTableLayer1
		numberOfBands = 32
	} else if !header.TestMpeg1() {
		allocation = allocationTableLayer2MPEG2
		numberOfBands = 30
	} else {
		sampleRateIndex := header.SampleRate()
		kbps := header.BitrateKbps()
		if mode != 3 { // mode != MODE_MONO
			kbps >>= 1
		}
		if kbps == 0 {
			kbps = 192
		}

		allocation = allocationTableLayer2MPEG1
		numberOfBands = 27
		if kbps < 56 {
			allocation = allocationTableLayer2MPEG1LowRate
			if sampleRateIndex == 2 {
				numberOfBands = 12
			} else {
				numberOfBands = 8
			}
		} else if kbps >= 96 && sampleRateIndex != 1 {
			numberOfBands = 30
		}
	}

	scaleFactorInfo.TotalBands = uint8(numberOfBands)
	scaleFactorInfo.StereoBands = uint8(min(stereoBands, numberOfBands))
	return allocation
}

var dequantizationTableLayer12 = [18 * 3]float32{
	9.53674316e-07 / 3.0, 7.56931807e-07 / 3.0, 6.00777173e-07 / 3.0,
	9.53674316e-07 / 7.0, 7.56931807e-07 / 7.0, 6.00777173e-07 / 7.0,
	9.53674316e-07 / 15.0, 7.56931807e-07 / 15.0, 6.00777173e-07 / 15.0,
	9.53674316e-07 / 31.0, 7.56931807e-07 / 31.0, 6.00777173e-07 / 31.0,
	9.53674316e-07 / 63.0, 7.56931807e-07 / 63.0, 6.00777173e-07 / 63.0,
	9.53674316e-07 / 127.0, 7.56931807e-07 / 127.0, 6.00777173e-07 / 127.0,
	9.53674316e-07 / 255.0, 7.56931807e-07 / 255.0, 6.00777173e-07 / 255.0,
	9.53674316e-07 / 511.0, 7.56931807e-07 / 511.0, 6.00777173e-07 / 511.0,
	9.53674316e-07 / 1023.0, 7.56931807e-07 / 1023.0, 6.00777173e-07 / 1023.0,
	9.53674316e-07 / 2047.0, 7.56931807e-07 / 2047.0, 6.00777173e-07 / 2047.0,
	9.53674316e-07 / 4095.0, 7.56931807e-07 / 4095.0, 6.00777173e-07 / 4095.0,
	9.53674316e-07 / 8191.0, 7.56931807e-07 / 8191.0, 6.00777173e-07 / 8191.0,
	9.53674316e-07 / 16383.0, 7.56931807e-07 / 16383.0, 6.00777173e-07 / 16383.0,
	9.53674316e-07 / 32767.0, 7.56931807e-07 / 32767.0, 6.00777173e-07 / 32767.0,
	9.53674316e-07 / 65535.0, 7.56931807e-07 / 65535.0, 6.00777173e-07 / 65535.0,
	9.53674316e-07 / 3.0, 7.56931807e-07 / 3.0, 6.00777173e-07 / 3.0,
	9.53674316e-07 / 5.0, 7.56931807e-07 / 5.0, 6.00777173e-07 / 5.0,
	9.53674316e-07 / 9.0, 7.56931807e-07 / 9.0, 6.00777173e-07 / 9.0,
}

func readScaleFactorsLayer12(bitStreamReader *BitStreamReader, bitAllocationTable []uint8, scaleFactorTransmissionCode []uint8, bands int, scaleFactors []float32) {
	bitAllocationIndex := 0
	scaleFactorsIndex := 0
	for i := 0; i < bands; i++ {
		var s float32 = 0
		bitAllocation := int(bitAllocationTable[bitAllocationIndex])
		bitAllocationIndex++
		mask := 0
		if bitAllocation != 0 {
			mask = 4 + int((19>>scaleFactorTransmissionCode[i])&3)
		}
		for m := 4; m > 0; m >>= 1 {
			if (mask & m) != 0 {
				b := int(bitStreamReader.getBits(6))
				s = dequantizationTableLayer12[bitAllocation*3-6+b%3] * float32(int(1<<21)>>(b/3))
			}
			scaleFactors[scaleFactorsIndex] = s
			scaleFactorsIndex++
		}
	}
}

var bitAllocationCodeTable = []byte{
	0, 17, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
	0, 17, 18, 3, 19, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 16,
	0, 17, 18, 3, 19, 4, 5, 16,
	0, 17, 18, 16,
	0, 17, 18, 19, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	0, 17, 18, 3, 19, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14,
	0, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
}

func readScaleFactorInfoLayer12(header Header, bitStreamReader *BitStreamReader, scaleFactorInfo *Layer12ScaleFactorInfo) {
	subbandAllocation := subbandAllocationTableLayer12(header, scaleFactorInfo)

	searchIndex := 0
	bitAllocationBits := 0
	bitAllocationCodeTableOffset := 0
	subbandAllocationIndex := 0

	for i := 0; i < int(scaleFactorInfo.TotalBands); i++ {
		var bitAllocation byte
		if i == searchIndex {
			sa := subbandAllocation[subbandAllocationIndex]
			subbandAllocationIndex++
			searchIndex += int(sa.BandCount)
			bitAllocationBits = int(sa.CodeTableWidth)
			bitAllocationCodeTableOffset = int(sa.TableOffset)
		}
		bits := int(bitStreamReader.getBits(bitAllocationBits))
		bitAllocation = bitAllocationCodeTable[bitAllocationCodeTableOffset+bits]
		scaleFactorInfo.BitAllocation[2*i] = bitAllocation
		if i < int(scaleFactorInfo.StereoBands) {
			bits = int(bitStreamReader.getBits(bitAllocationBits))
			bitAllocation = bitAllocationCodeTable[bitAllocationCodeTableOffset+bits]
		}
		if scaleFactorInfo.StereoBands != 0 {
			scaleFactorInfo.BitAllocation[2*i+1] = bitAllocation
		} else {
			scaleFactorInfo.BitAllocation[2*i+1] = 0
		}
	}

	for i := 0; i < 2*int(scaleFactorInfo.TotalBands); i++ {
		if scaleFactorInfo.BitAllocation[i] != 0 {
			if header.IsLayer1() {
				scaleFactorInfo.ScaleFactorTransmissionCode[i] = 2
			} else {
				scaleFactorInfo.ScaleFactorTransmissionCode[i] = byte(bitStreamReader.getBits(2))
			}
		} else {
			scaleFactorInfo.ScaleFactorTransmissionCode[i] = 6
		}
	}

	readScaleFactorsLayer12(bitStreamReader, scaleFactorInfo.BitAllocation[:], scaleFactorInfo.ScaleFactorTransmissionCode[:], int(scaleFactorInfo.TotalBands*2), scaleFactorInfo.ScaleFactors[:])

	for i := int(scaleFactorInfo.StereoBands); i < int(scaleFactorInfo.TotalBands); i++ {
		scaleFactorInfo.BitAllocation[2*i+1] = 0
	}
}

func dequantizeGranuleLayer12(granuleBuffer []float32, bitStreamReader *BitStreamReader, scaleFactorInfo *Layer12ScaleFactorInfo, groupSize int) int {
	channelOffset := SamplesPerGranuleLayer3
	for j := 0; j < 4; j++ {
		destinationOffset := groupSize * j
		for i := 0; i < 2*int(scaleFactorInfo.TotalBands); i++ {
			bitAllocation := int(scaleFactorInfo.BitAllocation[i])
			if bitAllocation != 0 {
				if bitAllocation < 17 {
					halfValue := (1 << (bitAllocation - 1)) - 1
					for k := 0; k < groupSize; k++ {
						granuleBuffer[destinationOffset+k] = float32(int(bitStreamReader.getBits(bitAllocation)) - halfValue)
					}
				} else {
					modulus := uint32((2 << (bitAllocation - 17)) + 1)
					codeValue := bitStreamReader.getBits(int(modulus + 2 - (modulus >> 3)))
					for k := 0; k < groupSize; k++ {
						granuleBuffer[destinationOffset+k] = float32(int(codeValue%modulus) - int(modulus/2))
						codeValue /= modulus
					}
				}
			}
			destinationOffset += channelOffset
			channelOffset = 18 - channelOffset
		}
	}
	return groupSize * 4
}

func applyScaleFactors384Layer12(scaleFactorInfo *Layer12ScaleFactorInfo, scaleFactors []float32, destination []float32) {
	copy(destination[SamplesPerGranuleLayer3+int(scaleFactorInfo.StereoBands)*SamplesPerSubbandLayer3:SamplesPerGranuleLayer3+int(scaleFactorInfo.TotalBands)*SamplesPerSubbandLayer3], destination[int(scaleFactorInfo.StereoBands)*SamplesPerSubbandLayer3:int(scaleFactorInfo.TotalBands)*SamplesPerSubbandLayer3])
	destinationOffset := 0
	scaleFactorsOffset := 0
	for i := 0; i < int(scaleFactorInfo.TotalBands); i++ {
		for k := 0; k < SamplesPerSubbandLayer3; k++ {
			destination[destinationOffset+k] *= scaleFactors[scaleFactorsOffset+0]
			destination[destinationOffset+k+SamplesPerGranuleLayer3] *= scaleFactors[scaleFactorsOffset+3]
		}
		destinationOffset += SamplesPerSubbandLayer3
		scaleFactorsOffset += 6
	}
}

type decoderWorkspace struct {
	bitStreamReader          BitStreamReader
	mainData                 [MaxBitreservoirBytes + MaxFreeFormatFrameSize]byte
	granuleInfo              [4]GranuleInfo
	granuleBuffer            [MaxGranuleBufferSize]float32
	scaleFactors             [MaxScalefactorBands]float32
	synthesisWorkspace       [2112]float32
	intensityStereoPositions [MaxChannels][MaxScalefactorBands]byte
}

func changeSignL3(granuleBuffer []float32) {
	for b := 0; b < NumSubbands; b += 2 {
		offset := (b + 1) * SamplesPerSubbandLayer3
		for i := 1; i < SamplesPerSubbandLayer3; i += 2 {
			granuleBuffer[offset+i] = -granuleBuffer[offset+i]
		}
	}
}

func reorderL3(granuleBuffer []float32, scratchBuffer []float32, scaleFactorBandTable []byte) {
	sourceIndex := 0
	destinationIndex := 0
	scaleFactorBandIndex := 0
	for {
		length := int(scaleFactorBandTable[scaleFactorBandIndex])
		if length == 0 {
			break
		}
		scaleFactorBandIndex += 3
		for i := 0; i < length; i++ {
			scratchBuffer[destinationIndex] = granuleBuffer[sourceIndex+0*length]
			destinationIndex++
			scratchBuffer[destinationIndex] = granuleBuffer[sourceIndex+1*length]
			destinationIndex++
			scratchBuffer[destinationIndex] = granuleBuffer[sourceIndex+2*length]
			destinationIndex++
			sourceIndex++
		}
		sourceIndex += 2 * length
	}
	copy(granuleBuffer[:destinationIndex], scratchBuffer[:destinationIndex])
}

var aa = [2][8]float32{
	{0.85749293, 0.88174200, 0.94962865, 0.98331459, 0.99551782, 0.99916056, 0.99989920, 0.99999316},
	{0.51449576, 0.47173197, 0.31337745, 0.18191320, 0.09457419, 0.04096558, 0.01419856, 0.00369997},
}

func antialiasLayer3(granuleBuffer []float32, numberOfBands int) {
	index := 0
	for ; numberOfBands > 0; numberOfBands-- {
		for i := 0; i < (SamplesPerSubbandLayer3/2)-1; i++ {
			u := granuleBuffer[index+SamplesPerSubbandLayer3+i]
			d := granuleBuffer[index+(SamplesPerSubbandLayer3-1)-i]
			granuleBuffer[index+SamplesPerSubbandLayer3+i] = u*aa[0][i] - d*aa[1][i]
			granuleBuffer[index+(SamplesPerSubbandLayer3-1)-i] = u*aa[1][i] + d*aa[0][i]
		}
		index += SamplesPerSubbandLayer3
	}
}

func stereoTopBandLayer3(rightChannel []float32, scaleFactorBandTable []byte, numberOfBands int, maxBand []int) {
	maxBand[0] = -1
	maxBand[1] = -1
	maxBand[2] = -1

	byteIndex := 0
	for i := 0; i < numberOfBands; i++ {
		scaleFactorBandValue := int(scaleFactorBandTable[i])
		for k := 0; k < scaleFactorBandValue; k += 2 {
			if rightChannel[byteIndex+k] != 0 || rightChannel[byteIndex+k+1] != 0 {
				maxBand[i%3] = i
				break
			}
		}
		byteIndex += scaleFactorBandValue
	}
}

func intensityStereoBandLayer3(leftChannel []float32, n int, kl float32, kr float32) {
	for i := 0; i < n; i++ {
		leftChannel[i+SamplesPerGranuleLayer3] = leftChannel[i] * kr
		leftChannel[i] = leftChannel[i] * kl
	}
}

func midsideStereoLayer3(leftChannel []float32, n int) {
	for i := 0; i < n; i++ {
		a := leftChannel[i]
		b := leftChannel[i+SamplesPerGranuleLayer3]
		leftChannel[i] = a + b
		leftChannel[i+SamplesPerGranuleLayer3] = a - b
	}
}

var pan = [14]float32{0, 1, 0.21132487, 0.78867513, 0.36602540, 0.63397460, 0.5, 0.5, 0.63397460, 0.36602540, 0.78867513, 0.21132487, 1, 0}

func stereoProcessLayer3(leftChannel []float32, intensityStereoPosition []byte, scaleFactorBandTable []byte, header Header, maxBand []int, mpeg2Shift int) {
	maxPos := 64
	if header.TestMpeg1() {
		maxPos = 7
	}

	byteIndex := 0
	for i := 0; ; i++ {
		scaleFactorBandValue := int(scaleFactorBandTable[i])
		if scaleFactorBandValue == 0 {
			break
		}
		ipos := int(intensityStereoPosition[i])
		if i > maxBand[i%3] && ipos < maxPos {
			var kl, kr float32
			var s float32 = 1.0
			if header.TestMidSideStereo() {
				s = 1.41421356
			}
			if header.TestMpeg1() {
				kl = pan[2*ipos]
				kr = pan[2*ipos+1]
			} else {
				kl = 1.0
				kr = layer3LdexpQ2(1.0, ((ipos+1)>>1)<<mpeg2Shift)
				if (ipos & 1) != 0 {
					kl = kr
					kr = 1.0
				}
			}
			intensityStereoBandLayer3(leftChannel[byteIndex:], scaleFactorBandValue, kl*s, kr*s)
		} else if header.TestMidSideStereo() {
			midsideStereoLayer3(leftChannel[byteIndex:], scaleFactorBandValue)
		}
		byteIndex += scaleFactorBandValue
	}
}

func readScaleFactorsLayer3(scaleFactors []byte, intensityStereoPosition []byte, scaleFactorSize []byte, scaleFactorCount []byte, bitStreamReader *BitStreamReader, scaleFactorSelectionInfo int) {
	scaleFactorsIndex := 0
	intensityStereoIndex := 0
	partitionIndex := 0
	for i := 0; i < 4 && scaleFactorCount[partitionIndex+i] != 0; i++ {
		cnt := int(scaleFactorCount[partitionIndex+i])
		if (scaleFactorSelectionInfo & 8) != 0 {
			copy(scaleFactors[scaleFactorsIndex:scaleFactorsIndex+cnt], intensityStereoPosition[intensityStereoIndex:intensityStereoIndex+cnt])
		} else {
			bits := int(scaleFactorSize[i])
			if bits == 0 {
				for k := 0; k < cnt; k++ {
					scaleFactors[scaleFactorsIndex+k] = 0
					intensityStereoPosition[intensityStereoIndex+k] = 0
				}
			} else {
				maxScf := -1
				if scaleFactorSelectionInfo < 0 {
					maxScf = (1 << bits) - 1
				}
				for k := 0; k < cnt; k++ {
					s := int(bitStreamReader.getBits(bits))
					if s == maxScf {
						intensityStereoPosition[intensityStereoIndex+k] = 255
					} else {
						intensityStereoPosition[intensityStereoIndex+k] = byte(s)
					}
					scaleFactors[scaleFactorsIndex+k] = byte(s)
				}
			}
		}
		intensityStereoIndex += cnt
		scaleFactorsIndex += cnt
		scaleFactorSelectionInfo *= 2
	}
	scaleFactors[scaleFactorsIndex+0] = 0
	scaleFactors[scaleFactorsIndex+1] = 0
	scaleFactors[scaleFactorsIndex+2] = 0
}

func intensityStereoLayer3(leftChannel []float32, intensityStereoPosition []byte, granule *GranuleInfo, granule1 *GranuleInfo, header Header) {
	var maxBand [3]int
	numberOfScaleFactorBands := int(granule.numberOfLongScaleFactorBands + granule.numberOfShortScaleFactorBands)
	maxBlocks := 1
	if granule.numberOfShortScaleFactorBands != 0 {
		maxBlocks = 3
	}

	stereoTopBandLayer3(leftChannel[SamplesPerGranuleLayer3:], granule.scaleFactorBandTable, numberOfScaleFactorBands, maxBand[:])
	if granule.numberOfLongScaleFactorBands != 0 {
		m := max(max(maxBand[0], maxBand[1]), maxBand[2])
		maxBand[0] = m
		maxBand[1] = m
		maxBand[2] = m
	}
	for i := 0; i < maxBlocks; i++ {
		defaultPosition := 0
		if header.TestMpeg1() {
			defaultPosition = 3
		}
		itop := numberOfScaleFactorBands - maxBlocks + i
		previousIndex := itop - maxBlocks
		if maxBand[i] >= previousIndex {
			intensityStereoPosition[itop] = byte(defaultPosition)
		} else {
			intensityStereoPosition[itop] = intensityStereoPosition[previousIndex]
		}
	}
	stereoProcessLayer3(leftChannel, intensityStereoPosition, granule.scaleFactorBandTable, header, maxBand[:], int(granule1.scaleFactorCompression&1))
}

var expfrac = [4]float32{9.31322575e-10, 7.83145814e-10, 6.58544508e-10, 5.53767716e-10}

func layer3LdexpQ2(y float32, expQ2 int) float32 {
	for expQ2 > 0 {
		e := min(120, expQ2)
		y *= expfrac[e&3] * float32(int(1<<30)>>(e>>2))
		expQ2 -= e
	}
	return y
}

var scfPartitions = [3][28]byte{
	{6, 5, 5, 5, 6, 5, 5, 5, 6, 5, 7, 3, 11, 10, 0, 0, 7, 7, 7, 0, 6, 6, 6, 3, 8, 8, 5, 0},
	{8, 9, 6, 12, 6, 9, 9, 9, 6, 9, 12, 6, 15, 18, 0, 0, 6, 15, 12, 0, 6, 12, 9, 6, 6, 18, 9, 0},
	{9, 9, 6, 12, 9, 9, 9, 9, 9, 9, 12, 6, 18, 18, 0, 0, 12, 12, 12, 0, 12, 9, 9, 6, 15, 12, 9, 0},
}

var scfcDecode = [16]byte{0, 1, 2, 3, 12, 5, 6, 7, 9, 10, 11, 13, 14, 15, 18, 19}
var scfMod = [24]byte{5, 5, 4, 4, 5, 5, 4, 1, 4, 3, 1, 1, 5, 6, 6, 1, 4, 4, 4, 1, 4, 3, 1, 1}

func decodeScaleFactorsLayer3(header Header, intensityStereoPosition []byte, bitStreamReader *BitStreamReader, granule *GranuleInfo, scaleFactors []float32, channelIndex int) {
	partitionIndex := 0
	if granule.numberOfShortScaleFactorBands != 0 {
		partitionIndex += 1
	}
	if granule.numberOfLongScaleFactorBands == 0 {
		partitionIndex += 1
	}
	scaleFactorPartition := scfPartitions[partitionIndex][:]

	var scaleFactorSize [4]byte
	var integerScaleFactors [MaxScalefactorBands]byte
	scaleFactorShift := int(granule.scaleFactorScale + 1)
	scaleFactorSelectionInfo := int(granule.scfsi)

	if header.TestMpeg1() {
		part := int(scfcDecode[granule.scaleFactorCompression])
		scaleFactorSize[0] = byte(part >> 2)
		scaleFactorSize[1] = byte(part >> 2)
		scaleFactorSize[2] = byte(part & 3)
		scaleFactorSize[3] = byte(part & 3)
	} else {
		ist := 0
		if header.TestIntensityStereo() && channelIndex != 0 {
			ist = 1
		}
		sfc := int(granule.scaleFactorCompression >> ist)
		k := ist * 12
		for ; sfc >= 0; k += 4 {
			modprod := 1
			for i := 3; i >= 0; i-- {
				scaleFactorSize[i] = byte((sfc / modprod) % int(scfMod[k+i]))
				modprod *= int(scfMod[k+i])
			}
			sfc -= modprod
		}
		scaleFactorPartition = scaleFactorPartition[k:]
		scaleFactorSelectionInfo = -16
	}

	readScaleFactorsLayer3(integerScaleFactors[:], intensityStereoPosition, scaleFactorSize[:], scaleFactorPartition, bitStreamReader, scaleFactorSelectionInfo)

	if granule.numberOfShortScaleFactorBands != 0 {
		sh := 3 - scaleFactorShift
		for i := 0; i < int(granule.numberOfShortScaleFactorBands); i += 3 {
			integerScaleFactors[int(granule.numberOfLongScaleFactorBands)+i+0] += granule.subblockGain[0] << sh
			integerScaleFactors[int(granule.numberOfLongScaleFactorBands)+i+1] += granule.subblockGain[1] << sh
			integerScaleFactors[int(granule.numberOfLongScaleFactorBands)+i+2] += granule.subblockGain[2] << sh
		}
	} else if granule.preemphasisFlag != 0 {
		preamp := [10]byte{1, 1, 1, 1, 2, 2, 3, 3, 3, 2}
		for i := 0; i < 10; i++ {
			integerScaleFactors[11+i] += preamp[i]
		}
	}

	gainExp := int(granule.globalGain) - 4 - 210
	if header.IsMidSideStereo() {
		gainExp -= 2
	}
	gain := layer3LdexpQ2(2048.0, 44-gainExp)
	for i := 0; i < int(granule.numberOfLongScaleFactorBands+granule.numberOfShortScaleFactorBands); i++ {
		scaleFactors[i] = layer3LdexpQ2(gain, int(integerScaleFactors[i])<<scaleFactorShift)
	}
}

func restoreReservoirLayer3(decoder *Decoder, bitStreamReader *BitStreamReader, workspace *decoderWorkspace, mainDataBegin int) error {
	frameBytes := int((bitStreamReader.bitLimit - bitStreamReader.bitPosition) / 8)
	bytesHave := min(decoder.BitReservoirBytes, mainDataBegin)

	startIdx := decoder.BitReservoirBytes - mainDataBegin
	if startIdx < 0 {
		startIdx = 0
	}
	copy(workspace.mainData[:], decoder.ReservoirBuffer[startIdx:startIdx+bytesHave])

	copy(workspace.mainData[bytesHave:], bitStreamReader.buffer[int(bitStreamReader.bitPosition/8):int(bitStreamReader.bitPosition/8)+frameBytes])

	workspace.bitStreamReader.buffer = workspace.mainData[:]
	workspace.bitStreamReader.bitPosition = 0
	workspace.bitStreamReader.bitLimit = int32((bytesHave + frameBytes) * 8)

	if decoder.BitReservoirBytes < mainDataBegin {
		return ErrInsufficientReservoir
	}
	return nil
}

func saveReservoirLayer3(decoder *Decoder, workspace *decoderWorkspace) {
	pos := int((workspace.bitStreamReader.bitPosition + 7) / 8)
	remains := int(workspace.bitStreamReader.bitLimit/8) - pos
	if remains > MaxBitreservoirBytes {
		pos += remains - MaxBitreservoirBytes
		remains = MaxBitreservoirBytes
	}
	if remains > 0 {
		copy(decoder.ReservoirBuffer[:remains], workspace.mainData[pos:pos+remains])
	}
	decoder.BitReservoirBytes = remains
}

var scfLong = [8][23]byte{
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54, 0},
	{12, 12, 12, 12, 12, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 76, 90, 2, 2, 2, 2, 2, 0},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54, 0},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 18, 22, 26, 32, 38, 46, 54, 62, 70, 76, 36, 0},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 8, 8, 10, 12, 16, 20, 24, 28, 34, 42, 50, 54, 76, 158, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 10, 12, 16, 18, 22, 28, 34, 40, 46, 54, 54, 192, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 8, 10, 12, 16, 20, 24, 30, 38, 46, 56, 68, 84, 102, 26, 0},
}

var scfShort = [8][MaxScalefactorBands + 1]byte{
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{8, 8, 8, 8, 8, 8, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 24, 24, 24, 28, 28, 28, 36, 36, 36, 2, 2, 2, 2, 2, 2, 2, 2, 2, 26, 26, 26, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 14, 14, 14, 18, 18, 18, 26, 26, 26, 32, 32, 32, 42, 42, 42, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 32, 32, 32, 44, 44, 44, 12, 12, 12, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 22, 22, 22, 30, 30, 30, 56, 56, 56, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 6, 6, 6, 10, 10, 10, 12, 12, 12, 14, 14, 14, 16, 16, 16, 20, 20, 20, 26, 26, 26, 66, 66, 66, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 26, 26, 26, 34, 34, 34, 42, 42, 42, 12, 12, 12, 0},
}

var scfMixed = [8][MaxScalefactorBands + 1]byte{
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{12, 12, 12, 4, 4, 4, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 24, 24, 24, 28, 28, 28, 36, 36, 36, 2, 2, 2, 2, 2, 2, 2, 2, 2, 26, 26, 26, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 14, 14, 14, 18, 18, 18, 26, 26, 26, 32, 32, 32, 42, 42, 42, 18, 18, 18, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 32, 32, 32, 44, 44, 44, 12, 12, 12, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 22, 22, 22, 30, 30, 30, 56, 56, 56, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 6, 6, 6, 10, 10, 10, 12, 12, 12, 14, 14, 14, 16, 16, 16, 20, 20, 20, 26, 26, 26, 66, 66, 66, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 26, 26, 26, 34, 34, 34, 42, 42, 42, 12, 12, 12, 0},
}

func readSideInformationLayer3(bitStreamReader *BitStreamReader, granuleInfo []GranuleInfo, header Header) int {
	sampleRateIndex := header.MySampleRate()
	if sampleRateIndex != 0 {
		sampleRateIndex -= 1
	}
	granuleCount := 2
	if header.IsMono() {
		granuleCount = 1
	}
	mainDataBegin := 0
	var scaleFactorSelectionInfo uint32 = 0

	if header.TestMpeg1() {
		granuleCount *= 2
		mainDataBegin = int(bitStreamReader.getBits(9))
		scaleFactorSelectionInfo = bitStreamReader.getBits(7 + granuleCount)
	} else {
		mainDataBegin = int(bitStreamReader.getBits(8+granuleCount) >> granuleCount)
	}

	part23Sum := 0
	initialGranuleCount := granuleCount
	for granuleIndex := 0; granuleIndex < initialGranuleCount; granuleIndex++ {
		if header.IsMono() {
			scaleFactorSelectionInfo <<= 4
		}
		granuleInfo[granuleIndex].part23Length = uint16(bitStreamReader.getBits(12))
		part23Sum += int(granuleInfo[granuleIndex].part23Length)
		granuleInfo[granuleIndex].bigValues = uint16(bitStreamReader.getBits(9))
		if granuleInfo[granuleIndex].bigValues*2 > SamplesPerGranuleLayer3 {
			return -1
		}
		granuleInfo[granuleIndex].globalGain = uint8(bitStreamReader.getBits(8))
		compressBits := 9
		if header.TestMpeg1() {
			compressBits = 4
		}
		granuleInfo[granuleIndex].scaleFactorCompression = uint16(bitStreamReader.getBits(compressBits))
		granuleInfo[granuleIndex].scaleFactorBandTable = scfLong[sampleRateIndex][:]
		granuleInfo[granuleIndex].numberOfLongScaleFactorBands = 22
		granuleInfo[granuleIndex].numberOfShortScaleFactorBands = 0
		if bitStreamReader.getBits(1) != 0 {
			granuleInfo[granuleIndex].blockType = uint8(bitStreamReader.getBits(2))
			if granuleInfo[granuleIndex].blockType == 0 {
				return -1
			}
			granuleInfo[granuleIndex].mixedBlockFlag = uint8(bitStreamReader.getBits(1))
			granuleInfo[granuleIndex].regionCount[0] = 7
			granuleInfo[granuleIndex].regionCount[1] = 255
			if granuleInfo[granuleIndex].blockType == 2 { // SHORT_BLOCK_TYPE = 2
				scaleFactorSelectionInfo &= 0x0F0F
				if granuleInfo[granuleIndex].mixedBlockFlag == 0 {
					granuleInfo[granuleIndex].regionCount[0] = 8
					granuleInfo[granuleIndex].scaleFactorBandTable = scfShort[sampleRateIndex][:]
					granuleInfo[granuleIndex].numberOfLongScaleFactorBands = 0
					granuleInfo[granuleIndex].numberOfShortScaleFactorBands = 39
				} else {
					granuleInfo[granuleIndex].scaleFactorBandTable = scfMixed[sampleRateIndex][:]
					if header.TestMpeg1() {
						granuleInfo[granuleIndex].numberOfLongScaleFactorBands = 8
					} else {
						granuleInfo[granuleIndex].numberOfLongScaleFactorBands = 6
					}
					granuleInfo[granuleIndex].numberOfShortScaleFactorBands = 30
				}
			}
			tables := bitStreamReader.getBits(10)
			tables <<= 5
			granuleInfo[granuleIndex].subblockGain[0] = uint8(bitStreamReader.getBits(3))
			granuleInfo[granuleIndex].subblockGain[1] = uint8(bitStreamReader.getBits(3))
			granuleInfo[granuleIndex].subblockGain[2] = uint8(bitStreamReader.getBits(3))
			granuleInfo[granuleIndex].tableSelect[0] = uint8(tables >> 10)
			granuleInfo[granuleIndex].tableSelect[1] = uint8((tables >> 5) & 31)
			granuleInfo[granuleIndex].tableSelect[2] = uint8(tables & 31)
		} else {
			granuleInfo[granuleIndex].blockType = 0
			granuleInfo[granuleIndex].mixedBlockFlag = 0
			tables := bitStreamReader.getBits(15)
			granuleInfo[granuleIndex].regionCount[0] = uint8(bitStreamReader.getBits(4))
			granuleInfo[granuleIndex].regionCount[1] = uint8(bitStreamReader.getBits(3))
			granuleInfo[granuleIndex].regionCount[2] = 255
			granuleInfo[granuleIndex].tableSelect[0] = uint8(tables >> 10)
			granuleInfo[granuleIndex].tableSelect[1] = uint8((tables >> 5) & 31)
			granuleInfo[granuleIndex].tableSelect[2] = uint8(tables & 31)
		}
		if header.TestMpeg1() {
			granuleInfo[granuleIndex].preemphasisFlag = uint8(bitStreamReader.getBits(1))
		} else {
			if granuleInfo[granuleIndex].scaleFactorCompression >= 500 {
				granuleInfo[granuleIndex].preemphasisFlag = 1
			} else {
				granuleInfo[granuleIndex].preemphasisFlag = 0
			}
		}
		granuleInfo[granuleIndex].scaleFactorScale = uint8(bitStreamReader.getBits(1))
		granuleInfo[granuleIndex].count1Table = uint8(bitStreamReader.getBits(1))
		granuleInfo[granuleIndex].scfsi = uint8((scaleFactorSelectionInfo >> 12) & 15)
		scaleFactorSelectionInfo <<= 4
	}

	if part23Sum+int(bitStreamReader.bitPosition) > int(bitStreamReader.bitLimit)+mainDataBegin*8 {
		return -1
	}

	return mainDataBegin
}

func decodeLayer3(decoder *Decoder, workspace *decoderWorkspace, granuleInfo []GranuleInfo, granuleInfoOffset int, channelCount int) {
	granuleBufferFlat := workspace.granuleBuffer[:]
	for ch := 0; ch < channelCount; ch++ {
		layer3grLimit := int(workspace.bitStreamReader.bitPosition) + int(granuleInfo[granuleInfoOffset+ch].part23Length)
		decodeScaleFactorsLayer3(decoder.Header, workspace.intensityStereoPositions[ch][:], &workspace.bitStreamReader, &granuleInfo[granuleInfoOffset+ch], workspace.scaleFactors[:], ch)
		huffmanDecodeLayer3(granuleBufferFlat[ch*SamplesPerGranuleLayer3:ch*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3], &workspace.bitStreamReader, &granuleInfo[granuleInfoOffset+ch], workspace.scaleFactors[:], layer3grLimit)
	}

	if decoder.Header.TestIntensityStereo() {
		intensityStereoLayer3(granuleBufferFlat, workspace.intensityStereoPositions[1][:], &granuleInfo[granuleInfoOffset], &granuleInfo[granuleInfoOffset+1], decoder.Header)
	} else if decoder.Header.IsMidSideStereo() {
		midsideStereoLayer3(granuleBufferFlat, SamplesPerGranuleLayer3)
	}

	for ch := 0; ch < channelCount; ch++ {
		gr := &granuleInfo[granuleInfoOffset+ch]
		aaBands := 30
		numberOfLongBands := 0
		if gr.mixedBlockFlag != 0 {
			numberOfLongBands = 2
		}
		if decoder.Header.MySampleRate() == 2 {
			numberOfLongBands <<= 1
		}
		if gr.blockType == 2 { // SHORT_BLOCK_TYPE = 2
			var scratchBuffer [SamplesPerGranuleLayer3]float32
			if gr.mixedBlockFlag != 0 {
				aaBands = numberOfLongBands - 1
			} else {
				aaBands = -1
			}
			reorderL3(granuleBufferFlat[ch*SamplesPerGranuleLayer3:ch*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3], scratchBuffer[:], gr.scaleFactorBandTable)
		}
		antialiasLayer3(granuleBufferFlat[ch*SamplesPerGranuleLayer3:], aaBands+1)
		L3Imdct(granuleBufferFlat[ch*SamplesPerGranuleLayer3:ch*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3], decoder.MdctOverlap[ch][:], int(gr.blockType), numberOfLongBands)
		changeSignL3(granuleBufferFlat[ch*SamplesPerGranuleLayer3 : ch*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3])
	}
}
