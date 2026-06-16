package mp3

type Layer12ScaleFactorInfo struct {
	ScaleFactors                [3 * 64]float32
	TotalBands                  uint8
	StereoBands                 uint8
	BitAllocation               [64]uint8
	ScaleFactorTransmissionCode [64]uint8
}

type Layer12SubBandAllocation struct {
	TableOffset    uint8
	CodeTableWidth uint8
	BandCount      uint8
}

var allocationTableLayer1 = []Layer12SubBandAllocation{{76, 4, 32}}
var allocationTableLayer2MPEG2 = []Layer12SubBandAllocation{{60, 4, 4}, {44, 3, 7}, {44, 2, 19}}
var allocationTableLayer2MPEG1 = []Layer12SubBandAllocation{{0, 4, 3}, {16, 4, 8}, {32, 3, 12}, {40, 2, 7}}
var allocationTableLayer2MPEG1LowRate = []Layer12SubBandAllocation{{44, 4, 2}, {44, 3, 10}}

const (
	stereoModeJointStereo = 1
	stereoModeMono        = 3
)

func subBandAllocationTableLayer12(header Header, scaleFactorInfo *Layer12ScaleFactorInfo) []Layer12SubBandAllocation {
	var allocationTable []Layer12SubBandAllocation
	stereoMode := header.StereoMode()
	stereoBands := 32

	switch stereoMode {
	case stereoModeMono:
		stereoBands = 0
	case stereoModeJointStereo:
		stereoBands = (header.StereoModeExt() << 2) + 4
	}

	numBands := 0
	if header.IsLayer1() {
		allocationTable = allocationTableLayer1
		numBands = 32
	} else if !header.IsMPEG1() {
		allocationTable = allocationTableLayer2MPEG2
		numBands = 30
	} else {
		sampleRateIndex := header.SampleRate()
		bitrateKbps := header.BitrateKbps()
		if stereoMode != stereoModeMono {
			bitrateKbps >>= 1
		}
		if bitrateKbps == 0 {
			bitrateKbps = 192
		}

		allocationTable = allocationTableLayer2MPEG1
		numBands = 27
		if bitrateKbps < 56 {
			allocationTable = allocationTableLayer2MPEG1LowRate
			if sampleRateIndex == 2 {
				numBands = 12
			} else {
				numBands = 8
			}
		} else if bitrateKbps >= 96 && sampleRateIndex != 1 {
			numBands = 30
		}
	}

	scaleFactorInfo.TotalBands = uint8(numBands)
	scaleFactorInfo.StereoBands = uint8(min(stereoBands, numBands))
	return allocationTable
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

func readScaleFactorsLayer12(bitReader *BitReader, bitAllocationTable []uint8, scaleFactorTransmissionCode []uint8, bands int, scaleFactors []float32) {
	bitAllocationIndex := 0
	scaleFactorIndex := 0
	for i := 0; i < bands; i++ {
		var scaleFactorValue float32 = 0
		bitAllocation := int(bitAllocationTable[bitAllocationIndex])
		bitAllocationIndex++
		transmissionMask := 0
		if bitAllocation != 0 {
			transmissionMask = 4 + int((19>>scaleFactorTransmissionCode[i])&3)
		}
		for bitMask := 4; bitMask > 0; bitMask >>= 1 {
			if (transmissionMask & bitMask) != 0 {
				scaleFactorCode := int(bitReader.getBits(6))
				scaleFactorValue = dequantizationTableLayer12[bitAllocation*3-6+scaleFactorCode%3] * float32(int(1<<21)>>(scaleFactorCode/3))
			}
			scaleFactors[scaleFactorIndex] = scaleFactorValue
			scaleFactorIndex++
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

func readScaleFactorInfoLayer12(header Header, bitReader *BitReader, scaleFactorInfo *Layer12ScaleFactorInfo) {
	subBandAllocation := subBandAllocationTableLayer12(header, scaleFactorInfo)

	nextBoundaryBand := 0
	allocationBitsCount := 0
	codeTableOffset := 0
	allocationTableIndex := 0

	for i := 0; i < int(scaleFactorInfo.TotalBands); i++ {
		var bitAllocation byte
		if i == nextBoundaryBand {
			sa := subBandAllocation[allocationTableIndex]
			allocationTableIndex++
			nextBoundaryBand += int(sa.BandCount)
			allocationBitsCount = int(sa.CodeTableWidth)
			codeTableOffset = int(sa.TableOffset)
		}
		rawBits := int(bitReader.getBits(allocationBitsCount))
		bitAllocation = bitAllocationCodeTable[codeTableOffset+rawBits]
		scaleFactorInfo.BitAllocation[2*i] = bitAllocation
		if i < int(scaleFactorInfo.StereoBands) {
			rawBits = int(bitReader.getBits(allocationBitsCount))
			bitAllocation = bitAllocationCodeTable[codeTableOffset+rawBits]
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
				scaleFactorInfo.ScaleFactorTransmissionCode[i] = byte(bitReader.getBits(2))
			}
		} else {
			scaleFactorInfo.ScaleFactorTransmissionCode[i] = 6
		}
	}

	readScaleFactorsLayer12(bitReader, scaleFactorInfo.BitAllocation[:], scaleFactorInfo.ScaleFactorTransmissionCode[:], int(scaleFactorInfo.TotalBands*2), scaleFactorInfo.ScaleFactors[:])

	for i := int(scaleFactorInfo.StereoBands); i < int(scaleFactorInfo.TotalBands); i++ {
		scaleFactorInfo.BitAllocation[2*i+1] = 0
	}
}

func dequantizeGranuleLayer12(granule []float32, bitReader *BitReader, scaleFactorInfo *Layer12ScaleFactorInfo, groupSize int) int {
	channelOffset := SamplesPerGranuleLayer3
	for groupIdx := 0; groupIdx < 4; groupIdx++ {
		destinationOffset := groupSize * groupIdx
		for i := 0; i < 2*int(scaleFactorInfo.TotalBands); i++ {
			bitAllocation := int(scaleFactorInfo.BitAllocation[i])
			if bitAllocation != 0 {
				if bitAllocation < 17 {
					halfRange := (1 << (bitAllocation - 1)) - 1
					for k := 0; k < groupSize; k++ {
						granule[destinationOffset+k] = float32(int(bitReader.getBits(bitAllocation)) - halfRange)
					}
				} else {
					steps := uint32((2 << (bitAllocation - 17)) + 1)
					groupedCode := bitReader.getBits(int(steps + 2 - (steps >> 3)))
					for k := 0; k < groupSize; k++ {
						granule[destinationOffset+k] = float32(int(groupedCode%steps) - int(steps/2))
						groupedCode /= steps
					}
				}
			}
			destinationOffset += channelOffset
			channelOffset = 18 - channelOffset
		}
	}
	return groupSize * 4
}

func applyScaleFactors384Layer12(scaleFactorInfo *Layer12ScaleFactorInfo, scaleFactors []float32, dest []float32) {
	copy(dest[SamplesPerGranuleLayer3+int(scaleFactorInfo.StereoBands)*SamplesPerSubBandLayer3:SamplesPerGranuleLayer3+int(scaleFactorInfo.TotalBands)*SamplesPerSubBandLayer3], dest[int(scaleFactorInfo.StereoBands)*SamplesPerSubBandLayer3:int(scaleFactorInfo.TotalBands)*SamplesPerSubBandLayer3])
	destIndex := 0
	scaleFactorIndex := 0
	for i := 0; i < int(scaleFactorInfo.TotalBands); i++ {
		for k := 0; k < SamplesPerSubBandLayer3; k++ {
			dest[destIndex+k] *= scaleFactors[scaleFactorIndex+0]
			dest[destIndex+k+SamplesPerGranuleLayer3] *= scaleFactors[scaleFactorIndex+3]
		}
		destIndex += SamplesPerSubBandLayer3
		scaleFactorIndex += 6
	}
}

type decoderWorkspace struct {
	bitReader                BitReader
	mainData                 [MaxBitReservoirBytes + MaxFreeFormatFrameSize]byte
	granuleInfo              [4]GranuleInfo
	granule                  [MaxGranuleBufferSize]float32
	scaleFactors             [MaxScaleFactorBands]float32
	synthesisWorkspace       [2112]float32
	intensityStereoPositions [MaxChannels][MaxScaleFactorBands]byte
}

func changeSignL3(granule []float32) {
	for band := 0; band < NumSubBands; band += 2 {
		bandOffset := (band + 1) * SamplesPerSubBandLayer3
		for i := 1; i < SamplesPerSubBandLayer3; i += 2 {
			granule[bandOffset+i] = -granule[bandOffset+i]
		}
	}
}

func reorderL3(granule []float32, scratch []float32, scaleFactorBandTable []byte) {
	sourceIndex := 0
	destIndex := 0
	scaleFactorBandIndex := 0
	for {
		length := int(scaleFactorBandTable[scaleFactorBandIndex])
		if length == 0 {
			break
		}
		scaleFactorBandIndex += 3
		for i := 0; i < length; i++ {
			scratch[destIndex] = granule[sourceIndex+0*length]
			destIndex++
			scratch[destIndex] = granule[sourceIndex+1*length]
			destIndex++
			scratch[destIndex] = granule[sourceIndex+2*length]
			destIndex++
			sourceIndex++
		}
		sourceIndex += 2 * length
	}
	copy(granule[:destIndex], scratch[:destIndex])
}

var aliasReductionCS = [8]float32{0.85749293, 0.88174200, 0.94962865, 0.98331459, 0.99551782, 0.99916056, 0.99989920, 0.99999316}
var aliasReductionCA = [8]float32{0.51449576, 0.47173197, 0.31337745, 0.18191320, 0.09457419, 0.04096558, 0.01419856, 0.00369997}

func antialiasLayer3(granule []float32, bandCount int) {
	bandOffset := 0
	for ; bandCount > 0; bandCount-- {
		for i := 0; i < (SamplesPerSubBandLayer3/2)-1; i++ {
			upperValue := granule[bandOffset+SamplesPerSubBandLayer3+i]
			lowerValue := granule[bandOffset+(SamplesPerSubBandLayer3-1)-i]
			granule[bandOffset+SamplesPerSubBandLayer3+i] = upperValue*aliasReductionCS[i] - lowerValue*aliasReductionCA[i]
			granule[bandOffset+(SamplesPerSubBandLayer3-1)-i] = upperValue*aliasReductionCA[i] + lowerValue*aliasReductionCS[i]
		}
		bandOffset += SamplesPerSubBandLayer3
	}
}

func stereoTopBandLayer3(rightChannel []float32, scaleFactorBandTable []byte, bandCount int, maxBand []int) {
	maxBand[0] = -1
	maxBand[1] = -1
	maxBand[2] = -1

	sampleIndex := 0
	for i := 0; i < bandCount; i++ {
		bandWidth := int(scaleFactorBandTable[i])
		for k := 0; k < bandWidth; k += 2 {
			if rightChannel[sampleIndex+k] != 0 || rightChannel[sampleIndex+k+1] != 0 {
				maxBand[i%3] = i
				break
			}
		}
		sampleIndex += bandWidth
	}
}

func intensityStereoBandLayer3(leftChannel []float32, bandWidth int, ratioLeft float32, ratioRight float32) {
	for i := 0; i < bandWidth; i++ {
		leftChannel[i+SamplesPerGranuleLayer3] = leftChannel[i] * ratioRight
		leftChannel[i] = leftChannel[i] * ratioLeft
	}
}

func midSideStereoLayer3(leftChannel []float32, bandWidth int) {
	for i := 0; i < bandWidth; i++ {
		leftSample := leftChannel[i]
		rightSample := leftChannel[i+SamplesPerGranuleLayer3]
		leftChannel[i] = leftSample + rightSample
		leftChannel[i+SamplesPerGranuleLayer3] = leftSample - rightSample
	}
}

var pan = [14]float32{0, 1, 0.21132487, 0.78867513, 0.36602540, 0.63397460, 0.5, 0.5, 0.63397460, 0.36602540, 0.78867513, 0.21132487, 1, 0}

func stereoProcessLayer3(leftChannel []float32, intensityStereoPosition []byte, scaleFactorBandTable []byte, header Header, maxBand []int, mpeg2Shift int) {
	maxPos := 64
	if header.IsMPEG1() {
		maxPos = 7
	}

	sampleOffset := 0
	for i := 0; ; i++ {
		bandWidth := int(scaleFactorBandTable[i])
		if bandWidth == 0 {
			break
		}
		intensityPosition := int(intensityStereoPosition[i])
		if i > maxBand[i%3] && intensityPosition < maxPos {
			var scaleLeft, scaleRight float32
			var msScaling float32 = 1.0
			if header.IsMidSideStereoEnabled() {
				msScaling = 1.41421356
			}
			if header.IsMPEG1() {
				scaleLeft = pan[2*intensityPosition]
				scaleRight = pan[2*intensityPosition+1]
			} else {
				scaleLeft = 1.0
				scaleRight = layer3LdexpQ2(1.0, ((intensityPosition+1)>>1)<<mpeg2Shift)
				if (intensityPosition & 1) != 0 {
					scaleLeft = scaleRight
					scaleRight = 1.0
				}
			}
			intensityStereoBandLayer3(leftChannel[sampleOffset:], bandWidth, scaleLeft*msScaling, scaleRight*msScaling)
		} else if header.IsMidSideStereoEnabled() {
			midSideStereoLayer3(leftChannel[sampleOffset:], bandWidth)
		}
		sampleOffset += bandWidth
	}
}

func readScaleFactorsLayer3(scaleFactors []byte, intensityStereoPosition []byte, scaleFactorSize []byte, scaleFactorCount []byte, bitReader *BitReader, scaleFactorSelectionInfo int) {
	scaleFactorIndex := 0
	intensityStereoIndex := 0
	partitionIndex := 0
	for i := 0; i < 4 && scaleFactorCount[partitionIndex+i] != 0; i++ {
		partitionSize := int(scaleFactorCount[partitionIndex+i])
		if (scaleFactorSelectionInfo & 8) != 0 {
			copy(scaleFactors[scaleFactorIndex:scaleFactorIndex+partitionSize], intensityStereoPosition[intensityStereoIndex:intensityStereoIndex+partitionSize])
		} else {
			bitLength := int(scaleFactorSize[i])
			if bitLength == 0 {
				for k := 0; k < partitionSize; k++ {
					scaleFactors[scaleFactorIndex+k] = 0
					intensityStereoPosition[intensityStereoIndex+k] = 0
				}
			} else {
				maxScaleFactor := -1
				if scaleFactorSelectionInfo < 0 {
					maxScaleFactor = (1 << bitLength) - 1
				}
				for k := 0; k < partitionSize; k++ {
					scfValue := int(bitReader.getBits(bitLength))
					if scfValue == maxScaleFactor {
						intensityStereoPosition[intensityStereoIndex+k] = 255
					} else {
						intensityStereoPosition[intensityStereoIndex+k] = byte(scfValue)
					}
					scaleFactors[scaleFactorIndex+k] = byte(scfValue)
				}
			}
		}
		intensityStereoIndex += partitionSize
		scaleFactorIndex += partitionSize
		scaleFactorSelectionInfo *= 2
	}
	scaleFactors[scaleFactorIndex+0] = 0
	scaleFactors[scaleFactorIndex+1] = 0
	scaleFactors[scaleFactorIndex+2] = 0
}

func intensityStereoLayer3(leftChannel []float32, intensityStereoPosition []byte, granule *GranuleInfo, granule1 *GranuleInfo, header Header) {
	var maxBand [3]int
	numScaleFactorBands := int(granule.longScaleFactorBandCount + granule.shortScaleFactorBandCount)
	numSubBlocks := 1
	if granule.shortScaleFactorBandCount != 0 {
		numSubBlocks = 3
	}

	stereoTopBandLayer3(leftChannel[SamplesPerGranuleLayer3:], granule.scaleFactorBandTable, numScaleFactorBands, maxBand[:])
	if granule.longScaleFactorBandCount != 0 {
		m := max(max(maxBand[0], maxBand[1]), maxBand[2])
		maxBand[0] = m
		maxBand[1] = m
		maxBand[2] = m
	}
	for i := 0; i < numSubBlocks; i++ {
		defaultIntensityPos := 0
		if header.IsMPEG1() {
			defaultIntensityPos = 3
		}
		subBlockBandIndex := numScaleFactorBands - numSubBlocks + i
		prevBandIndex := subBlockBandIndex - numSubBlocks
		if maxBand[i] >= prevBandIndex {
			intensityStereoPosition[subBlockBandIndex] = byte(defaultIntensityPos)
		} else {
			intensityStereoPosition[subBlockBandIndex] = intensityStereoPosition[prevBandIndex]
		}
	}
	stereoProcessLayer3(leftChannel, intensityStereoPosition, granule.scaleFactorBandTable, header, maxBand[:], int(granule1.scaleFactorCompression&1))
}

var ldexpFractionalScales = [4]float32{9.31322575e-10, 7.83145814e-10, 6.58544508e-10, 5.53767716e-10}

func layer3LdexpQ2(val float32, exponent int) float32 {
	for exponent > 0 {
		shiftStep := min(120, exponent)
		val *= ldexpFractionalScales[shiftStep&3] * float32(int(1<<30)>>(shiftStep>>2))
		exponent -= shiftStep
	}
	return val
}

var scaleFactorBandPartitionSizes = [3][28]byte{
	{6, 5, 5, 5, 6, 5, 5, 5, 6, 5, 7, 3, 11, 10, 0, 0, 7, 7, 7, 0, 6, 6, 6, 3, 8, 8, 5, 0},
	{8, 9, 6, 12, 6, 9, 9, 9, 6, 9, 12, 6, 15, 18, 0, 0, 6, 15, 12, 0, 6, 12, 9, 6, 6, 18, 9, 0},
	{9, 9, 6, 12, 9, 9, 9, 9, 9, 9, 12, 6, 18, 18, 0, 0, 12, 12, 12, 0, 12, 9, 9, 6, 15, 12, 9, 0},
}

var mpeg1ScaleFactorCompressDecodeTable = [16]byte{0, 1, 2, 3, 12, 5, 6, 7, 9, 10, 11, 13, 14, 15, 18, 19}
var mpeg2ScaleFactorModuli = [24]byte{5, 5, 4, 4, 5, 5, 4, 1, 4, 3, 1, 1, 5, 6, 6, 1, 4, 4, 4, 1, 4, 3, 1, 1}

func decodeScaleFactorsLayer3(header Header, intensityStereoPosition []byte, bitReader *BitReader, granule *GranuleInfo, scaleFactors []float32, channelIndex int) {
	partitionIndex := 0
	if granule.shortScaleFactorBandCount != 0 {
		partitionIndex += 1
	}
	if granule.longScaleFactorBandCount == 0 {
		partitionIndex += 1
	}
	partitionBands := scaleFactorBandPartitionSizes[partitionIndex][:]

	var scaleFactorSize [4]byte
	var integerScaleFactors [MaxScaleFactorBands]byte
	scfShift := int(granule.scaleFactorScale + 1)
	scfSelectInfo := int(granule.scaleFactorSelectionInfo)

	if header.IsMPEG1() {
		scfCompressCode := int(mpeg1ScaleFactorCompressDecodeTable[granule.scaleFactorCompression])
		scaleFactorSize[0] = byte(scfCompressCode >> 2)
		scaleFactorSize[1] = byte(scfCompressCode >> 2)
		scaleFactorSize[2] = byte(scfCompressCode & 3)
		scaleFactorSize[3] = byte(scfCompressCode & 3)
	} else {
		intensityStereoShift := 0
		if header.IsIntensityStereoEnabled() && channelIndex != 0 {
			intensityStereoShift = 1
		}
		scfCompress := int(granule.scaleFactorCompression >> intensityStereoShift)
		partitionTableOffset := intensityStereoShift * 12
		for ; scfCompress >= 0; partitionTableOffset += 4 {
			modulusProduct := 1
			for i := 3; i >= 0; i-- {
				scaleFactorSize[i] = byte((scfCompress / modulusProduct) % int(mpeg2ScaleFactorModuli[partitionTableOffset+i]))
				modulusProduct *= int(mpeg2ScaleFactorModuli[partitionTableOffset+i])
			}
			scfCompress -= modulusProduct
		}
		partitionBands = partitionBands[partitionTableOffset:]
		scfSelectInfo = -16
	}

	readScaleFactorsLayer3(integerScaleFactors[:], intensityStereoPosition, scaleFactorSize[:], partitionBands, bitReader, scfSelectInfo)

	if granule.shortScaleFactorBandCount != 0 {
		gainShift := 3 - scfShift
		for i := 0; i < int(granule.shortScaleFactorBandCount); i += 3 {
			integerScaleFactors[int(granule.longScaleFactorBandCount)+i+0] += granule.subBlockGain[0] << gainShift
			integerScaleFactors[int(granule.longScaleFactorBandCount)+i+1] += granule.subBlockGain[1] << gainShift
			integerScaleFactors[int(granule.longScaleFactorBandCount)+i+2] += granule.subBlockGain[2] << gainShift
		}
	} else if granule.preEmphasisFlag != 0 {
		preEmphasisTable := [10]byte{1, 1, 1, 1, 2, 2, 3, 3, 3, 2}
		for i := 0; i < 10; i++ {
			integerScaleFactors[11+i] += preEmphasisTable[i]
		}
	}

	gainExponent := int(granule.globalGain) - 4 - 210
	if header.IsMidSideStereo() {
		gainExponent -= 2
	}
	baseGain := layer3LdexpQ2(2048.0, 44-gainExponent)
	for i := 0; i < int(granule.longScaleFactorBandCount+granule.shortScaleFactorBandCount); i++ {
		scaleFactors[i] = layer3LdexpQ2(baseGain, int(integerScaleFactors[i])<<scfShift)
	}
}

func restoreReservoirLayer3(decoder *Decoder, bitReader *BitReader, workspace *decoderWorkspace, mainDataOffset int) error {
	remainingFrameBytes := int((bitReader.limit - bitReader.position) / 8)
	availableReservoirBytes := min(decoder.BitReservoirBytes, mainDataOffset)

	reservoirStartIndex := decoder.BitReservoirBytes - mainDataOffset
	if reservoirStartIndex < 0 {
		reservoirStartIndex = 0
	}
	copy(workspace.mainData[:], decoder.ReservoirBuffer[reservoirStartIndex:reservoirStartIndex+availableReservoirBytes])

	copy(workspace.mainData[availableReservoirBytes:], bitReader.buffer[int(bitReader.position/8):int(bitReader.position/8)+remainingFrameBytes])

	workspace.bitReader.buffer = workspace.mainData[:]
	workspace.bitReader.position = 0
	workspace.bitReader.limit = int32((availableReservoirBytes + remainingFrameBytes) * 8)

	if decoder.BitReservoirBytes < mainDataOffset {
		return ErrInsufficientReservoir
	}
	return nil
}

func saveReservoirLayer3(decoder *Decoder, workspace *decoderWorkspace) {
	bufferPosition := int((workspace.bitReader.position + 7) / 8)
	remainingBytes := int(workspace.bitReader.limit/8) - bufferPosition
	if remainingBytes > MaxBitReservoirBytes {
		bufferPosition += remainingBytes - MaxBitReservoirBytes
		remainingBytes = MaxBitReservoirBytes
	}
	if remainingBytes > 0 {
		copy(decoder.ReservoirBuffer[:remainingBytes], workspace.mainData[bufferPosition:bufferPosition+remainingBytes])
	}
	decoder.BitReservoirBytes = remainingBytes
}

var scaleFactorBandWidthsLongBlocks = [8][23]byte{
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54, 0},
	{12, 12, 12, 12, 12, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 76, 90, 2, 2, 2, 2, 2, 0},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54, 0},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 18, 22, 26, 32, 38, 46, 54, 62, 70, 76, 36, 0},
	{6, 6, 6, 6, 6, 6, 8, 10, 12, 14, 16, 20, 24, 28, 32, 38, 46, 52, 60, 68, 58, 54, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 8, 8, 10, 12, 16, 20, 24, 28, 34, 42, 50, 54, 76, 158, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 10, 12, 16, 18, 22, 28, 34, 40, 46, 54, 54, 192, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 8, 10, 12, 16, 20, 24, 30, 38, 46, 56, 68, 84, 102, 26, 0},
}

var scaleFactorBandWidthsShortBlocks = [8][MaxScaleFactorBands + 1]byte{
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{8, 8, 8, 8, 8, 8, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 24, 24, 24, 28, 28, 28, 36, 36, 36, 2, 2, 2, 2, 2, 2, 2, 2, 2, 26, 26, 26, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 14, 14, 14, 18, 18, 18, 26, 26, 26, 32, 32, 32, 42, 42, 42, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 32, 32, 32, 44, 44, 44, 12, 12, 12, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 22, 22, 22, 30, 30, 30, 56, 56, 56, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 6, 6, 6, 10, 10, 10, 12, 12, 12, 14, 14, 14, 16, 16, 16, 20, 20, 20, 26, 26, 26, 66, 66, 66, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 26, 26, 26, 34, 34, 34, 42, 42, 42, 12, 12, 12, 0},
}

var scaleFactorBandWidthsMixedBlocks = [8][MaxScaleFactorBands + 1]byte{
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{12, 12, 12, 4, 4, 4, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 24, 24, 24, 28, 28, 28, 36, 36, 36, 2, 2, 2, 2, 2, 2, 2, 2, 2, 26, 26, 26, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 14, 14, 14, 18, 18, 18, 26, 26, 26, 32, 32, 32, 42, 42, 42, 18, 18, 18, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 32, 32, 32, 44, 44, 44, 12, 12, 12, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 22, 22, 22, 30, 30, 30, 56, 56, 56, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 6, 6, 6, 10, 10, 10, 12, 12, 12, 14, 14, 14, 16, 16, 16, 20, 20, 20, 26, 26, 26, 66, 66, 66, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 26, 26, 26, 34, 34, 34, 42, 42, 42, 12, 12, 12, 0},
}

func readSideInfoLayer3(bitReader *BitReader, granuleInfo []GranuleInfo, header Header) int {
	sampleRateIndex := header.UnifiedSampleRateIndex()
	if sampleRateIndex != 0 {
		sampleRateIndex -= 1
	}
	numGranules := 2
	if header.IsMono() {
		numGranules = 1
	}
	mainDataOffset := 0
	var scfSelectInfo uint32 = 0

	if header.IsMPEG1() {
		numGranules *= 2
		mainDataOffset = int(bitReader.getBits(9))
		scfSelectInfo = bitReader.getBits(7 + numGranules)
	} else {
		mainDataOffset = int(bitReader.getBits(8+numGranules) >> numGranules)
	}

	part23LengthSum := 0
	numGranulesTotal := numGranules
	for granuleIndex := 0; granuleIndex < numGranulesTotal; granuleIndex++ {
		if header.IsMono() {
			scfSelectInfo <<= 4
		}
		granuleInfo[granuleIndex].part23Length = uint16(bitReader.getBits(12))
		part23LengthSum += int(granuleInfo[granuleIndex].part23Length)
		granuleInfo[granuleIndex].bigValues = uint16(bitReader.getBits(9))
		if granuleInfo[granuleIndex].bigValues*2 > SamplesPerGranuleLayer3 {
			return -1
		}
		granuleInfo[granuleIndex].globalGain = uint8(bitReader.getBits(8))
		compressionBitLength := 9
		if header.IsMPEG1() {
			compressionBitLength = 4
		}
		granuleInfo[granuleIndex].scaleFactorCompression = uint16(bitReader.getBits(compressionBitLength))
		granuleInfo[granuleIndex].scaleFactorBandTable = scaleFactorBandWidthsLongBlocks[sampleRateIndex][:]
		granuleInfo[granuleIndex].longScaleFactorBandCount = 22
		granuleInfo[granuleIndex].shortScaleFactorBandCount = 0
		if bitReader.getBits(1) != 0 {
			granuleInfo[granuleIndex].blockType = uint8(bitReader.getBits(2))
			if granuleInfo[granuleIndex].blockType == 0 {
				return -1
			}
			granuleInfo[granuleIndex].mixedBlockFlag = uint8(bitReader.getBits(1))
			granuleInfo[granuleIndex].regionCount[0] = 7
			granuleInfo[granuleIndex].regionCount[1] = 255
			if granuleInfo[granuleIndex].blockType == 2 { // SHORT_BLOCK_TYPE = 2
				scfSelectInfo &= 0x0F0F
				if granuleInfo[granuleIndex].mixedBlockFlag == 0 {
					granuleInfo[granuleIndex].regionCount[0] = 8
					granuleInfo[granuleIndex].scaleFactorBandTable = scaleFactorBandWidthsShortBlocks[sampleRateIndex][:]
					granuleInfo[granuleIndex].longScaleFactorBandCount = 0
					granuleInfo[granuleIndex].shortScaleFactorBandCount = 39
				} else {
					granuleInfo[granuleIndex].scaleFactorBandTable = scaleFactorBandWidthsMixedBlocks[sampleRateIndex][:]
					if header.IsMPEG1() {
						granuleInfo[granuleIndex].longScaleFactorBandCount = 8
					} else {
						granuleInfo[granuleIndex].longScaleFactorBandCount = 6
					}
					granuleInfo[granuleIndex].shortScaleFactorBandCount = 30
				}
			}
			tableSelectionCode := bitReader.getBits(10)
			tableSelectionCode <<= 5
			granuleInfo[granuleIndex].subBlockGain[0] = uint8(bitReader.getBits(3))
			granuleInfo[granuleIndex].subBlockGain[1] = uint8(bitReader.getBits(3))
			granuleInfo[granuleIndex].subBlockGain[2] = uint8(bitReader.getBits(3))
			granuleInfo[granuleIndex].tableSelect[0] = uint8(tableSelectionCode >> 10)
			granuleInfo[granuleIndex].tableSelect[1] = uint8((tableSelectionCode >> 5) & 31)
			granuleInfo[granuleIndex].tableSelect[2] = uint8(tableSelectionCode & 31)
		} else {
			granuleInfo[granuleIndex].blockType = 0
			granuleInfo[granuleIndex].mixedBlockFlag = 0
			tableSelectionCode := bitReader.getBits(15)
			granuleInfo[granuleIndex].regionCount[0] = uint8(bitReader.getBits(4))
			granuleInfo[granuleIndex].regionCount[1] = uint8(bitReader.getBits(3))
			granuleInfo[granuleIndex].regionCount[2] = 255
			granuleInfo[granuleIndex].tableSelect[0] = uint8(tableSelectionCode >> 10)
			granuleInfo[granuleIndex].tableSelect[1] = uint8((tableSelectionCode >> 5) & 31)
			granuleInfo[granuleIndex].tableSelect[2] = uint8(tableSelectionCode & 31)
		}
		if header.IsMPEG1() {
			granuleInfo[granuleIndex].preEmphasisFlag = uint8(bitReader.getBits(1))
		} else {
			if granuleInfo[granuleIndex].scaleFactorCompression >= 500 {
				granuleInfo[granuleIndex].preEmphasisFlag = 1
			} else {
				granuleInfo[granuleIndex].preEmphasisFlag = 0
			}
		}
		granuleInfo[granuleIndex].scaleFactorScale = uint8(bitReader.getBits(1))
		granuleInfo[granuleIndex].count1Table = uint8(bitReader.getBits(1))
		granuleInfo[granuleIndex].scaleFactorSelectionInfo = uint8((scfSelectInfo >> 12) & 15)
		scfSelectInfo <<= 4
	}

	if part23LengthSum+int(bitReader.position) > int(bitReader.limit)+mainDataOffset*8 {
		return -1
	}

	return mainDataOffset
}

func decodeLayer3(decoder *Decoder, workspace *decoderWorkspace, granuleInfo []GranuleInfo, granuleInfoOffset int, channelCount int) {
	granuleSamples := workspace.granule[:]
	for channel := 0; channel < channelCount; channel++ {
		granuleBitLimit := int(workspace.bitReader.position) + int(granuleInfo[granuleInfoOffset+channel].part23Length)
		decodeScaleFactorsLayer3(decoder.Header, workspace.intensityStereoPositions[channel][:], &workspace.bitReader, &granuleInfo[granuleInfoOffset+channel], workspace.scaleFactors[:], channel)
		huffmanDecodeLayer3(granuleSamples[channel*SamplesPerGranuleLayer3:channel*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3], &workspace.bitReader, &granuleInfo[granuleInfoOffset+channel], workspace.scaleFactors[:], granuleBitLimit)
	}

	if decoder.Header.IsIntensityStereoEnabled() {
		intensityStereoLayer3(granuleSamples, workspace.intensityStereoPositions[1][:], &granuleInfo[granuleInfoOffset], &granuleInfo[granuleInfoOffset+1], decoder.Header)
	} else if decoder.Header.IsMidSideStereo() {
		midSideStereoLayer3(granuleSamples, SamplesPerGranuleLayer3)
	}

	for channel := 0; channel < channelCount; channel++ {
		grInfo := &granuleInfo[granuleInfoOffset+channel]
		antialiasBands := 30
		numLongBands := 0
		if grInfo.mixedBlockFlag != 0 {
			numLongBands = 2
		}
		if decoder.Header.UnifiedSampleRateIndex() == 2 {
			numLongBands <<= 1
		}
		if grInfo.blockType == 2 { // SHORT_BLOCK_TYPE = 2
			var scratchBuffer [SamplesPerGranuleLayer3]float32
			if grInfo.mixedBlockFlag != 0 {
				antialiasBands = numLongBands - 1
			} else {
				antialiasBands = -1
			}
			reorderL3(granuleSamples[channel*SamplesPerGranuleLayer3:channel*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3], scratchBuffer[:], grInfo.scaleFactorBandTable)
		}
		antialiasLayer3(granuleSamples[channel*SamplesPerGranuleLayer3:], antialiasBands+1)
		L3Imdct(granuleSamples[channel*SamplesPerGranuleLayer3:channel*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3], decoder.MdctOverlap[channel][:], int(grInfo.blockType), numLongBands)
		changeSignL3(granuleSamples[channel*SamplesPerGranuleLayer3 : channel*SamplesPerGranuleLayer3+SamplesPerGranuleLayer3])
	}
}
