package layer3

import (
	"github.com/godexture/codec-mp3/internal/mp3/bits"
	"github.com/godexture/format-mp3/header"
)

func reorder(granule []float32, scratch []float32, scaleFactorBandTable []byte) {
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

var ldexpFractionalScales = [4]float32{9.31322575e-10, 7.83145814e-10, 6.58544508e-10, 5.53767716e-10}

func ldexpQ2(val float32, exponent int) float32 {
	for exponent > 0 {
		shiftStep := min(120, exponent)
		val *= ldexpFractionalScales[shiftStep&3] * float32(int(1<<30)>>(shiftStep>>2))
		exponent -= shiftStep
	}
	return val
}

func readScaleFactors(scaleFactors []byte, intensityStereoPosition []byte, scaleFactorSize []byte, scaleFactorCount []byte, bitReader *bits.BitReader, scaleFactorSelectionInfo int) {
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
					scfValue := int(bitReader.GetBits(bitLength))
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

var scaleFactorBandPartitionSizes = [3][28]byte{
	{6, 5, 5, 5, 6, 5, 5, 5, 6, 5, 7, 3, 11, 10, 0, 0, 7, 7, 7, 0, 6, 6, 6, 3, 8, 8, 5, 0},
	{8, 9, 6, 12, 6, 9, 9, 9, 6, 9, 12, 6, 15, 18, 0, 0, 6, 15, 12, 0, 6, 12, 9, 6, 6, 18, 9, 0},
	{9, 9, 6, 12, 9, 9, 9, 9, 9, 9, 12, 6, 18, 18, 0, 0, 12, 12, 12, 0, 12, 9, 9, 6, 15, 12, 9, 0},
}

var mpeg1CompressDecodeTable = [16]byte{0, 1, 2, 3, 12, 5, 6, 7, 9, 10, 11, 13, 14, 15, 18, 19}
var mpeg2Moduli = [24]byte{5, 5, 4, 4, 5, 5, 4, 1, 4, 3, 1, 1, 5, 6, 6, 1, 4, 4, 4, 1, 4, 3, 1, 1}

func DecodeScaleFactors(h header.Header, intensityStereoPosition []byte, bitReader *bits.BitReader, granule *GranuleInfo, scaleFactors []float32, channelIndex int) {
	partitionIndex := 0
	if granule.ShortScaleFactorBandCount != 0 {
		partitionIndex += 1
	}
	if granule.LongScaleFactorBandCount == 0 {
		partitionIndex += 1
	}
	partitionBands := scaleFactorBandPartitionSizes[partitionIndex][:]

	var scaleFactorSize [4]byte
	var integerScaleFactors [maxScaleFactorBands]byte
	scfShift := int(granule.ScaleFactorScale + 1)
	scfSelectInfo := int(granule.ScaleFactorSelectionInfo)

	if h.IsMPEG1() {
		scfCompressCode := int(mpeg1CompressDecodeTable[granule.ScaleFactorCompression])
		scaleFactorSize[0] = byte(scfCompressCode >> 2)
		scaleFactorSize[1] = byte(scfCompressCode >> 2)
		scaleFactorSize[2] = byte(scfCompressCode & 3)
		scaleFactorSize[3] = byte(scfCompressCode & 3)
	} else {
		intensityStereoShift := 0
		if h.IsIntensityStereoEnabled() && channelIndex != 0 {
			intensityStereoShift = 1
		}
		scfCompress := int(granule.ScaleFactorCompression >> intensityStereoShift)
		partitionTableOffset := intensityStereoShift * 12
		for ; scfCompress >= 0; partitionTableOffset += 4 {
			modulusProduct := 1
			for i := 3; i >= 0; i-- {
				scaleFactorSize[i] = byte((scfCompress / modulusProduct) % int(mpeg2Moduli[partitionTableOffset+i]))
				modulusProduct *= int(mpeg2Moduli[partitionTableOffset+i])
			}
			scfCompress -= modulusProduct
		}
		partitionBands = partitionBands[partitionTableOffset:]
		scfSelectInfo = -16
	}

	readScaleFactors(integerScaleFactors[:], intensityStereoPosition, scaleFactorSize[:], partitionBands, bitReader, scfSelectInfo)

	if granule.ShortScaleFactorBandCount != 0 {
		gainShift := 3 - scfShift
		for i := 0; i < int(granule.ShortScaleFactorBandCount); i += 3 {
			integerScaleFactors[int(granule.LongScaleFactorBandCount)+i+0] += granule.SubBlockGain[0] << gainShift
			integerScaleFactors[int(granule.LongScaleFactorBandCount)+i+1] += granule.SubBlockGain[1] << gainShift
			integerScaleFactors[int(granule.LongScaleFactorBandCount)+i+2] += granule.SubBlockGain[2] << gainShift
		}
	} else if granule.PreEmphasisFlag != 0 {
		preEmphasisTable := [10]byte{1, 1, 1, 1, 2, 2, 3, 3, 3, 2}
		for i := 0; i < 10; i++ {
			integerScaleFactors[11+i] += preEmphasisTable[i]
		}
	}

	gainExponent := int(granule.GlobalGain) - 4 - 210
	if h.IsMidSideStereo() {
		gainExponent -= 2
	}
	baseGain := ldexpQ2(2048.0, 44-gainExponent)
	for i := 0; i < int(granule.LongScaleFactorBandCount+granule.ShortScaleFactorBandCount); i++ {
		scaleFactors[i] = ldexpQ2(baseGain, int(integerScaleFactors[i])<<scfShift)
	}
}
