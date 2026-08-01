package layer3

import (
	"github.com/godexture/godec/plugin/mp3/header"
	"github.com/godexture/godec/sdk/bits"
	"github.com/godexture/godec/sdk/buffer"
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
	maxMainDataBytes     = 2304
	maxScaleFactorBands  = 39
	reservoirCapacity    = 2 * (maxBitReservoirBytes + maxMainDataBytes)
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
	mdctOverlap [maxChannels][(SamplesPerSubBand / 2) * SubBandCount]float32
	reservoir   buffer.Ring[byte]
}

func (d *Decoder) Init() {
	reservoir := d.reservoir
	*d = Decoder{reservoir: reservoir}
	if d.reservoir.Cap() < reservoirCapacity {
		d.reservoir = buffer.NewRing[byte](reservoirCapacity)
	} else {
		d.reservoir.Reset()
	}
}

type Workspace struct {
	bitReader                bits.Reader
	granuleInfo              [4]GranuleInfo
	granule                  [maxGranuleBufferSize]float32
	scaleFactors             [maxScaleFactorBands]float32
	intensityStereoPositions [maxChannels][maxScaleFactorBands]byte
}
