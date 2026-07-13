package layer3

import (
	"github.com/godexture/format-mp3/header"
	"github.com/godexture/sdk/bits"
)

const (
	SamplesPerGranule = header.SamplesPerGranuleLayer3
	SamplesPerSubBand = header.SamplesPerSubBandLayer3
	SubBandCount      = 32
)

const (
	maxChannels          = header.MaxChannels
	maxGranuleBufferSize = SamplesPerGranule * maxChannels
	maxBitReservoirBytes = 511
	maxScaleFactorBands  = 39
)

// GranuleInfo matches the layout of granule information.
type GranuleInfo struct {
	ScaleFactorBandTable      []byte
	Part23Length              uint16
	BigValues                 uint16
	ScaleFactorCompression    uint16
	GlobalGain                uint8
	BlockType                 uint8
	MixedBlockFlag            uint8
	LongScaleFactorBandCount  uint8
	ShortScaleFactorBandCount uint8
	TableSelect               [3]uint8
	RegionCount               [3]uint8
	SubBlockGain              [3]uint8
	PreEmphasisFlag           uint8
	ScaleFactorScale          uint8
	Count1Table               uint8
	ScaleFactorSelectionInfo  uint8
}

type Decoder struct {
	mdctOverlap       [maxChannels][(SamplesPerSubBand / 2) * SubBandCount]float32
	bitReservoirBytes int
	reservoirBuffer   [maxBitReservoirBytes]byte
}

func (d *Decoder) Init() {
	*d = Decoder{}
}

type Workspace struct {
	bitReader                bits.Reader
	mainData                 [maxBitReservoirBytes + 2304]byte // 2304 is MaxFreeFormatFrameSize
	granuleInfo              [4]GranuleInfo
	granule                  [maxGranuleBufferSize]float32
	scaleFactors             [maxScaleFactorBands]float32
	intensityStereoPositions [maxChannels][maxScaleFactorBands]byte
}
