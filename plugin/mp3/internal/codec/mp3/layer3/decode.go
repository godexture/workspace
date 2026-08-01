package layer3

import (
	"github.com/godexture/godec/plugin/mp3/internal/codec/mp3/domain"
	"github.com/godexture/godec/plugin/mp3/header"
	"github.com/godexture/godec/sdk/bits"
)

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

var scaleFactorBandWidthsShortBlocks = [8][maxScaleFactorBands + 1]byte{
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{8, 8, 8, 8, 8, 8, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 24, 24, 24, 28, 28, 28, 36, 36, 36, 2, 2, 2, 2, 2, 2, 2, 2, 2, 26, 26, 26, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 14, 14, 14, 18, 18, 18, 26, 26, 26, 32, 32, 32, 42, 42, 42, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 32, 32, 32, 44, 44, 44, 12, 12, 12, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 22, 22, 22, 30, 30, 30, 56, 56, 56, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 6, 6, 6, 10, 10, 10, 12, 12, 12, 14, 14, 14, 16, 16, 16, 20, 20, 20, 26, 26, 26, 66, 66, 66, 0},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 6, 6, 6, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 26, 26, 26, 34, 34, 34, 42, 42, 42, 12, 12, 12, 0},
}

var scaleFactorBandWidthsMixedBlocks = [8][maxScaleFactorBands + 1]byte{
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{12, 12, 12, 4, 4, 4, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 24, 24, 24, 28, 28, 28, 36, 36, 36, 2, 2, 2, 2, 2, 2, 2, 2, 2, 26, 26, 26, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 14, 14, 14, 18, 18, 18, 26, 26, 26, 32, 32, 32, 42, 42, 42, 18, 18, 18, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 32, 32, 32, 44, 44, 44, 12, 12, 12, 0},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 24, 24, 24, 30, 30, 30, 40, 40, 40, 18, 18, 18, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 8, 8, 8, 10, 10, 10, 12, 12, 12, 14, 14, 14, 18, 18, 18, 22, 22, 22, 30, 30, 30, 56, 56, 56, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 6, 6, 6, 10, 10, 10, 12, 12, 12, 14, 14, 14, 16, 16, 16, 20, 20, 20, 26, 26, 26, 66, 66, 66, 0},
	{4, 4, 4, 4, 4, 4, 6, 6, 4, 4, 4, 6, 6, 6, 8, 8, 8, 12, 12, 12, 16, 16, 16, 20, 20, 20, 26, 26, 26, 34, 34, 34, 42, 42, 42, 12, 12, 12, 0},
}

// readSideInfo reads the granule side info that precedes a frame's main
// data. Reads use the Checked tier (ReadBits64) because this runs once per
// frame (a few dozen calls) rather than per sample: a truncated read is
// reported as domain.ErrBitStreamUnderflow, distinct from the structurally
// invalid cases below (domain.ErrInvalidSideInfo), which mirror the
// original implementation's sentinel -1 return.
func readSideInfo(bitReader *bits.Reader, granuleInfo []GranuleInfo, h header.Header) (int, error) {
	var readErr error
	read := func(n uint8) uint64 {
		if readErr != nil {
			return 0
		}
		v, err := bitReader.ReadBits64(n)
		if err != nil {
			readErr = err
		}
		return v
	}

	sampleRateIndex := h.UnifiedSampleRateIndex()
	if sampleRateIndex != 0 {
		sampleRateIndex -= 1
	}
	numGranules := 2
	if h.IsMono() {
		numGranules = 1
	}
	mainDataOffset := 0
	var scfSelectInfo uint32 = 0

	if h.IsMPEG1() {
		numGranules *= 2
		mainDataOffset = int(read(9))
		scfSelectInfo = uint32(read(uint8(7 + numGranules)))
	} else {
		mainDataOffset = int(read(uint8(8+numGranules)) >> numGranules)
	}

	part23LengthSum := 0
	numGranulesTotal := numGranules
	for granuleIndex := 0; granuleIndex < numGranulesTotal; granuleIndex++ {
		if h.IsMono() {
			scfSelectInfo <<= 4
		}
		granuleInfo[granuleIndex].ScaleFactorBandTable = scaleFactorBandWidthsLongBlocks[sampleRateIndex][:]
		granuleInfo[granuleIndex].Part23Length = uint16(read(12))
		part23LengthSum += int(granuleInfo[granuleIndex].Part23Length)
		granuleInfo[granuleIndex].BigValues = uint16(read(9))
		if granuleInfo[granuleIndex].BigValues*2 > SamplesPerGranule {
			return -1, domain.ErrInvalidSideInfo
		}
		granuleInfo[granuleIndex].GlobalGain = uint8(read(8))
		compressionBitLength := uint8(9)
		if h.IsMPEG1() {
			compressionBitLength = 4
		}
		granuleInfo[granuleIndex].ScaleFactorCompression = uint16(read(compressionBitLength))
		granuleInfo[granuleIndex].LongScaleFactorBandCount = 22
		granuleInfo[granuleIndex].ShortScaleFactorBandCount = 0
		if read(1) != 0 {
			granuleInfo[granuleIndex].BlockType = uint8(read(2))
			if granuleInfo[granuleIndex].BlockType == 0 {
				return -1, domain.ErrInvalidSideInfo
			}
			granuleInfo[granuleIndex].MixedBlockFlag = uint8(read(1))
			granuleInfo[granuleIndex].RegionCount[0] = 7
			granuleInfo[granuleIndex].RegionCount[1] = 255
			if granuleInfo[granuleIndex].BlockType == 2 { // SHORT_BLOCK_TYPE = 2
				scfSelectInfo &= 0x0F0F
				if granuleInfo[granuleIndex].MixedBlockFlag == 0 {
					granuleInfo[granuleIndex].RegionCount[0] = 8
					granuleInfo[granuleIndex].ScaleFactorBandTable = scaleFactorBandWidthsShortBlocks[sampleRateIndex][:]
					granuleInfo[granuleIndex].LongScaleFactorBandCount = 0
					granuleInfo[granuleIndex].ShortScaleFactorBandCount = 39
				} else {
					granuleInfo[granuleIndex].ScaleFactorBandTable = scaleFactorBandWidthsMixedBlocks[sampleRateIndex][:]
					if h.IsMPEG1() {
						granuleInfo[granuleIndex].LongScaleFactorBandCount = 8
					} else {
						granuleInfo[granuleIndex].LongScaleFactorBandCount = 6
					}
					granuleInfo[granuleIndex].ShortScaleFactorBandCount = 30
				}
			}
			tableSelectionCode := read(10)
			tableSelectionCode <<= 5
			granuleInfo[granuleIndex].SubBlockGain[0] = uint8(read(3))
			granuleInfo[granuleIndex].SubBlockGain[1] = uint8(read(3))
			granuleInfo[granuleIndex].SubBlockGain[2] = uint8(read(3))
			granuleInfo[granuleIndex].TableSelect[0] = uint8(tableSelectionCode >> 10)
			granuleInfo[granuleIndex].TableSelect[1] = uint8((tableSelectionCode >> 5) & 31)
			granuleInfo[granuleIndex].TableSelect[2] = uint8(tableSelectionCode & 31)
		} else {
			granuleInfo[granuleIndex].BlockType = 0
			granuleInfo[granuleIndex].MixedBlockFlag = 0
			tableSelectionCode := read(15)
			granuleInfo[granuleIndex].RegionCount[0] = uint8(read(4))
			granuleInfo[granuleIndex].RegionCount[1] = uint8(read(3))
			granuleInfo[granuleIndex].RegionCount[2] = 255
			granuleInfo[granuleIndex].TableSelect[0] = uint8(tableSelectionCode >> 10)
			granuleInfo[granuleIndex].TableSelect[1] = uint8((tableSelectionCode >> 5) & 31)
			granuleInfo[granuleIndex].TableSelect[2] = uint8(tableSelectionCode & 31)
		}
		if h.IsMPEG1() {
			granuleInfo[granuleIndex].PreEmphasisFlag = uint8(read(1))
		} else {
			if granuleInfo[granuleIndex].ScaleFactorCompression >= 500 {
				granuleInfo[granuleIndex].PreEmphasisFlag = 1
			} else {
				granuleInfo[granuleIndex].PreEmphasisFlag = 0
			}
		}
		granuleInfo[granuleIndex].ScaleFactorScale = uint8(read(1))
		granuleInfo[granuleIndex].Count1Table = uint8(read(1))
		granuleInfo[granuleIndex].ScaleFactorSelectionInfo = uint8((scfSelectInfo >> 12) & 15)
		scfSelectInfo <<= 4
	}

	if readErr != nil {
		return -1, domain.ErrBitStreamUnderflow
	}

	if part23LengthSum > int(bitReader.Remaining())+mainDataOffset*8 {
		return -1, domain.ErrInvalidSideInfo
	}

	return mainDataOffset, nil
}

func changeSign(granule []float32) {
	for band := 0; band < SubBandCount; band += 2 {
		bandOffset := (band + 1) * SamplesPerSubBand
		for i := 1; i < SamplesPerSubBand; i += 2 {
			granule[bandOffset+i] = -granule[bandOffset+i]
		}
	}
}

func decodeGranule(decoder *Decoder, workspace *Workspace, granuleInfo []GranuleInfo, granuleInfoOffset int, channelCount int, h header.Header) {
	granuleSamples := workspace.granule[:]
	for channel := 0; channel < channelCount; channel++ {
		granuleBitLimit := int(workspace.bitReader.Position()) + int(granuleInfo[granuleInfoOffset+channel].Part23Length)
		DecodeScaleFactors(h, workspace.intensityStereoPositions[channel][:], &workspace.bitReader, &granuleInfo[granuleInfoOffset+channel], workspace.scaleFactors[:], channel)
		HuffmanDecode(granuleSamples[channel*SamplesPerGranule:channel*SamplesPerGranule+SamplesPerGranule], &workspace.bitReader, &granuleInfo[granuleInfoOffset+channel], workspace.scaleFactors[:], granuleBitLimit)
	}

	if h.IsIntensityStereoEnabled() {
		IntensityStereo(granuleSamples, workspace.intensityStereoPositions[1][:], &granuleInfo[granuleInfoOffset], &granuleInfo[granuleInfoOffset+1], h)
	} else if h.IsMidSideStereo() {
		midSideStereo(granuleSamples, SamplesPerGranule)
	}

	for channel := 0; channel < channelCount; channel++ {
		grInfo := &granuleInfo[granuleInfoOffset+channel]
		antialiasBands := 30
		numLongBands := 0
		if grInfo.MixedBlockFlag != 0 {
			numLongBands = 2
		}
		if h.UnifiedSampleRateIndex() == 2 {
			numLongBands <<= 1
		}
		if grInfo.BlockType == 2 { // SHORT_BLOCK_TYPE = 2
			var scratchBuffer [SamplesPerGranule]float32
			if grInfo.MixedBlockFlag != 0 {
				antialiasBands = numLongBands - 1
			} else {
				antialiasBands = -1
			}
			reorder(granuleSamples[channel*SamplesPerGranule:channel*SamplesPerGranule+SamplesPerGranule], scratchBuffer[:], grInfo.ScaleFactorBandTable)
		}
		Antialias(granuleSamples[channel*SamplesPerGranule:], antialiasBands+1)
		Imdct(granuleSamples[channel*SamplesPerGranule:channel*SamplesPerGranule+SamplesPerGranule], decoder.mdctOverlap[channel][:], int(grInfo.BlockType), numLongBands)
		changeSign(granuleSamples[channel*SamplesPerGranule : channel*SamplesPerGranule+SamplesPerGranule])
	}
}

func Decode(
	decoder *Decoder,
	workspace *Workspace,
	bitStreamFrame *bits.Reader,
	channels int,
	h header.Header,
	synthesize func(granule []float32, pcmOffset int),
) error {
	mainDataBegin, err := readSideInfo(bitStreamFrame, workspace.granuleInfo[:], h)
	if err != nil {
		decoder.Init()
		return err
	}
	if err := restoreReservoir(decoder, bitStreamFrame, workspace, mainDataBegin); err != nil {
		saveReservoir(decoder, workspace)
		return err
	}
	pcmOffset := 0
	granuleLimit := 1
	if h.IsMPEG1() {
		granuleLimit = 2
	}
	for granuleIndex := 0; granuleIndex < granuleLimit; granuleIndex++ {
		workspace.granule = [maxGranuleBufferSize]float32{}
		decodeGranule(decoder, workspace, workspace.granuleInfo[:], granuleIndex*channels, channels, h)
		synthesize(workspace.granule[:], pcmOffset)
		pcmOffset += SamplesPerGranule * channels
	}
	if workspace.bitReader.Overrun() {
		decoder.Init()
		return domain.ErrBitStreamUnderflow
	}
	saveReservoir(decoder, workspace)
	return nil
}
