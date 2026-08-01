package layer12

import (
	"github.com/godexture/godec/plugin/mp3/header"
	"github.com/godexture/godec/sdk/bits"
)

type ScaleFactorInfo struct {
	ScaleFactors                [3 * 64]float32
	TotalBands                  uint8
	StereoBands                 uint8
	BitAllocation               [64]uint8
	ScaleFactorTransmissionCode [64]uint8
}

type SubBandAllocation struct {
	TableOffset    uint8
	CodeTableWidth uint8
	BandCount      uint8
}

var allocationTable1 = []SubBandAllocation{{76, 4, 32}}
var allocationTable2MPEG2 = []SubBandAllocation{{60, 4, 4}, {44, 3, 7}, {44, 2, 19}}
var allocationTable2MPEG1 = []SubBandAllocation{{0, 4, 3}, {16, 4, 8}, {32, 3, 12}, {40, 2, 7}}
var allocationTable2MPEG1LowRate = []SubBandAllocation{{44, 4, 2}, {44, 3, 10}}

func subBandAllocationTable(h header.Header, scaleFactorInfo *ScaleFactorInfo) []SubBandAllocation {
	var allocationTable []SubBandAllocation
	stereoMode := h.StereoMode()
	stereoBands := 32

	switch stereoMode {
	case stereoModeMono:
		stereoBands = 0
	case stereoModeJointStereo:
		stereoBands = (h.StereoModeExt() << 2) + 4
	}

	numBands := 0
	if h.IsLayer1() {
		allocationTable = allocationTable1
		numBands = 32
	} else if !h.IsMPEG1() {
		allocationTable = allocationTable2MPEG2
		numBands = 30
	} else {
		sampleRateIndex := h.SampleRate()
		bitrateKbps := h.BitrateKbps()
		if stereoMode != stereoModeMono {
			bitrateKbps >>= 1
		}
		if bitrateKbps == 0 {
			bitrateKbps = 192
		}

		allocationTable = allocationTable2MPEG1
		numBands = 27
		if bitrateKbps < 56 {
			allocationTable = allocationTable2MPEG1LowRate
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

var dequantizationTable = [18 * 3]float32{
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

func readScaleFactors(bitReader *bits.Reader, bitAllocationTable []uint8, scaleFactorTransmissionCode []uint8, bands int, scaleFactors []float32) {
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
				scaleFactorCode := int(bitReader.Bits32(6))
				scaleFactorValue = dequantizationTable[bitAllocation*3-6+scaleFactorCode%3] * float32(int(1<<21)>>(scaleFactorCode/3))
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

func ReadScaleFactorInfo(h header.Header, bitReader *bits.Reader, scaleFactorInfo *ScaleFactorInfo) {
	subBandAllocation := subBandAllocationTable(h, scaleFactorInfo)

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
		rawBits := int(bitReader.Bits32(uint8(allocationBitsCount)))
		bitAllocation = bitAllocationCodeTable[codeTableOffset+rawBits]
		scaleFactorInfo.BitAllocation[2*i] = bitAllocation
		if i < int(scaleFactorInfo.StereoBands) {
			rawBits = int(bitReader.Bits32(uint8(allocationBitsCount)))
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
			if h.IsLayer1() {
				scaleFactorInfo.ScaleFactorTransmissionCode[i] = 2
			} else {
				scaleFactorInfo.ScaleFactorTransmissionCode[i] = byte(bitReader.Bits32(2))
			}
		} else {
			scaleFactorInfo.ScaleFactorTransmissionCode[i] = 6
		}
	}

	readScaleFactors(bitReader, scaleFactorInfo.BitAllocation[:], scaleFactorInfo.ScaleFactorTransmissionCode[:], int(scaleFactorInfo.TotalBands*2), scaleFactorInfo.ScaleFactors[:])

	for i := int(scaleFactorInfo.StereoBands); i < int(scaleFactorInfo.TotalBands); i++ {
		scaleFactorInfo.BitAllocation[2*i+1] = 0
	}
}

func DequantizeGranule(granule []float32, bitReader *bits.Reader, scaleFactorInfo *ScaleFactorInfo, groupSize int) int {
	channelOffset := header.SamplesPerGranuleLayer3
	for groupIdx := 0; groupIdx < 4; groupIdx++ {
		destinationOffset := groupSize * groupIdx
		for i := 0; i < 2*int(scaleFactorInfo.TotalBands); i++ {
			bitAllocation := int(scaleFactorInfo.BitAllocation[i])
			if bitAllocation != 0 {
				if bitAllocation < 17 {
					halfRange := (1 << (bitAllocation - 1)) - 1
					for k := 0; k < groupSize; k++ {
						granule[destinationOffset+k] = float32(int(bitReader.Bits32(uint8(bitAllocation))) - halfRange)
					}
				} else {
					steps := uint32((2 << (bitAllocation - 17)) + 1)
					groupedCode := bitReader.Bits32(uint8(steps + 2 - (steps >> 3)))
					for k := 0; k < groupSize; k++ {
						granule[destinationOffset+k] = float32(int(groupedCode%steps) - int(steps/2))
						groupedCode /= steps
					}
				}
			}
			destinationOffset += channelOffset
			channelOffset = synthesisBufferStride - channelOffset
		}
	}
	return groupSize * 4
}

func ApplyScaleFactors384(scaleFactorInfo *ScaleFactorInfo, scaleFactors []float32, dest []float32) {
	copy(dest[header.SamplesPerGranuleLayer3+int(scaleFactorInfo.StereoBands)*synthesisBufferStride:header.SamplesPerGranuleLayer3+int(scaleFactorInfo.TotalBands)*synthesisBufferStride], dest[int(scaleFactorInfo.StereoBands)*synthesisBufferStride:int(scaleFactorInfo.TotalBands)*synthesisBufferStride])
	destIndex := 0
	scaleFactorIndex := 0
	for i := 0; i < int(scaleFactorInfo.TotalBands); i++ {
		for k := 0; k < synthesisBufferStride; k++ {
			dest[destIndex+k] *= scaleFactors[scaleFactorIndex+0]
			dest[destIndex+k+header.SamplesPerGranuleLayer3] *= scaleFactors[scaleFactorIndex+3]
		}
		destIndex += synthesisBufferStride
		scaleFactorIndex += 6
	}
}
