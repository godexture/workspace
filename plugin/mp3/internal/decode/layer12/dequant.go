package layer12

import (
	"github.com/godexture/godec/plugin/mp3/internal/frame"
	"github.com/godexture/godec/sdk/bits"
)

type scaleFactorInfo struct {
	scaleFactors     [3 * subbands * channels]float32
	totalBands       uint8
	stereoBands      uint8
	bitAllocation    [subbands * channels]uint8
	transmissionCode [subbands * channels]uint8
}

type subbandAllocation struct {
	tableOffset uint8
	codeWidth   uint8
	bandCount   uint8
}

var allocationTable1 = [...]subbandAllocation{{76, 4, 32}}
var allocationTable2MPEG2 = [...]subbandAllocation{{60, 4, 4}, {44, 3, 7}, {44, 2, 19}}
var allocationTable2MPEG1 = [...]subbandAllocation{{0, 4, 3}, {16, 4, 8}, {32, 3, 12}, {40, 2, 7}}
var allocationTable2MPEG1LowRate = [...]subbandAllocation{{44, 4, 2}, {44, 3, 10}}

func subbandAllocationTable(header frame.Header, scaleFactors *scaleFactorInfo) []subbandAllocation {
	stereoBands := subbands
	switch header.ChannelMode() {
	case frame.Mono:
		stereoBands = 0
	case frame.JointStereo:
		stereoBands = int(header.ModeExtension())*4 + 4
	}

	var allocationTable []subbandAllocation
	numberOfBands := 0
	switch {
	case header.Layer() == frame.LayerI:
		allocationTable = allocationTable1[:]
		numberOfBands = subbands
	case header.Version() != frame.VersionMPEG1:
		allocationTable = allocationTable2MPEG2[:]
		numberOfBands = 30
	default:
		bitrateKbps := header.BitrateKbps()
		if header.ChannelMode() != frame.Mono {
			bitrateKbps >>= 1
		}
		if bitrateKbps == 0 {
			bitrateKbps = 192
		}

		allocationTable = allocationTable2MPEG1[:]
		numberOfBands = 27
		if bitrateKbps < 56 {
			allocationTable = allocationTable2MPEG1LowRate[:]
			if header.SampleRateIndex() == 2 {
				numberOfBands = 12
			} else {
				numberOfBands = 8
			}
		} else if bitrateKbps >= 96 && header.SampleRateIndex() != 1 {
			numberOfBands = 30
		}
	}

	scaleFactors.totalBands = uint8(numberOfBands)
	scaleFactors.stereoBands = uint8(min(stereoBands, numberOfBands))
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

var bitAllocationCodeTable = [...]byte{
	0, 17, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
	0, 17, 18, 3, 19, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 16,
	0, 17, 18, 3, 19, 4, 5, 16,
	0, 17, 18, 16,
	0, 17, 18, 19, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	0, 17, 18, 3, 19, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14,
	0, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
}

func readScaleFactorInfo(header frame.Header, bitReader *bits.Reader, scaleFactors *scaleFactorInfo) {
	subbandAllocation := subbandAllocationTable(header, scaleFactors)

	nextBoundaryBand := 0
	allocationBits := 0
	codeTableOffset := 0
	allocationTableIndex := 0
	for band := 0; band < int(scaleFactors.totalBands); band++ {
		if band == nextBoundaryBand {
			allocation := subbandAllocation[allocationTableIndex]
			allocationTableIndex++
			nextBoundaryBand += int(allocation.bandCount)
			allocationBits = int(allocation.codeWidth)
			codeTableOffset = int(allocation.tableOffset)
		}
		rawBits := int(bitReader.Bits32(uint8(allocationBits)))
		bitAllocation := bitAllocationCodeTable[codeTableOffset+rawBits]
		scaleFactors.bitAllocation[2*band] = bitAllocation
		if band < int(scaleFactors.stereoBands) {
			rawBits = int(bitReader.Bits32(uint8(allocationBits)))
			bitAllocation = bitAllocationCodeTable[codeTableOffset+rawBits]
		}
		if scaleFactors.stereoBands != 0 {
			scaleFactors.bitAllocation[2*band+1] = bitAllocation
		} else {
			scaleFactors.bitAllocation[2*band+1] = 0
		}
	}

	for i := 0; i < 2*int(scaleFactors.totalBands); i++ {
		if scaleFactors.bitAllocation[i] != 0 {
			if header.Layer() == frame.LayerI {
				scaleFactors.transmissionCode[i] = 2
			} else {
				scaleFactors.transmissionCode[i] = byte(bitReader.Bits32(2))
			}
		} else {
			scaleFactors.transmissionCode[i] = 6
		}
	}

	readScaleFactors(bitReader, scaleFactors)
	for band := int(scaleFactors.stereoBands); band < int(scaleFactors.totalBands); band++ {
		scaleFactors.bitAllocation[2*band+1] = 0
	}
}

func readScaleFactors(bitReader *bits.Reader, scaleFactors *scaleFactorInfo) {
	scaleFactorIndex := 0
	for i := 0; i < 2*int(scaleFactors.totalBands); i++ {
		var scaleFactor float32
		bitAllocation := int(scaleFactors.bitAllocation[i])
		transmissionMask := 0
		if bitAllocation != 0 {
			transmissionMask = 4 + int((19>>scaleFactors.transmissionCode[i])&3)
		}
		for bitMask := 4; bitMask > 0; bitMask >>= 1 {
			if transmissionMask&bitMask != 0 {
				scaleFactorCode := int(bitReader.Bits32(6))
				scaleFactor = dequantizationTable[bitAllocation*3-6+scaleFactorCode%3] * float32(int(1<<21)>>(scaleFactorCode/3))
			}
			scaleFactors.scaleFactors[scaleFactorIndex] = scaleFactor
			scaleFactorIndex++
		}
	}
}

func dequantize(granule []float32, bitReader *bits.Reader, scaleFactors *scaleFactorInfo, groupSize int) {
	channelOffset := granuleSamples
	for group := 0; group < 4; group++ {
		destinationOffset := groupSize * group
		for i := 0; i < 2*int(scaleFactors.totalBands); i++ {
			bitAllocation := int(scaleFactors.bitAllocation[i])
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
			channelOffset = granuleStride - channelOffset
		}
	}
}

func applyScaleFactors(scaleFactors *scaleFactorInfo, block int, destination []float32) {
	copy(
		destination[granuleSamples+int(scaleFactors.stereoBands)*granuleStride:granuleSamples+int(scaleFactors.totalBands)*granuleStride],
		destination[int(scaleFactors.stereoBands)*granuleStride:int(scaleFactors.totalBands)*granuleStride],
	)
	scaleFactorIndex := 0
	for band := 0; band < int(scaleFactors.totalBands); band++ {
		for sample := 0; sample < granuleStride; sample++ {
			destination[band*granuleStride+sample] *= scaleFactors.scaleFactors[block+scaleFactorIndex]
			destination[band*granuleStride+sample+granuleSamples] *= scaleFactors.scaleFactors[block+scaleFactorIndex+3]
		}
		scaleFactorIndex += 6
	}
}
