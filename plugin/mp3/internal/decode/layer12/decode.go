// Package layer12 decodes MPEG audio Layer I and II subband samples.
package layer12

import (
	"errors"
	"io"

	"github.com/godexture/godec/plugin/mp3/internal/frame"
	"github.com/godexture/godec/sdk/bits"
)

const (
	channels       = 2
	subbands       = 32
	granuleStride  = 18
	granuleSamples = granuleStride * subbands
	blocks         = 3
)

var errLayer = errors.New("layer12: header is not Layer I or II")

// Decoder retains the bounded decoded blocks used while a Layer I or II frame
// is parsed.
type Decoder struct {
	granules [blocks][granuleSamples * channels]float32
}

// Decode parses one Layer I or II payload and invokes synthesize only after it
// has been fully parsed. The callback's block index is zero for Layer I and
// runs from zero through two for Layer II. Callback slices belong to d and
// remain valid until the next call to Decode.
func (d *Decoder) Decode(bitReader *bits.Reader, header frame.Header, synthesize func(granule []float32, block int)) error {
	clear(d.granules[:])

	blockCount := 1
	switch header.Layer() {
	case frame.LayerI:
	case frame.LayerII:
		blockCount = blocks
	default:
		return errLayer
	}

	var scaleFactors scaleFactorInfo
	readScaleFactorInfo(header, bitReader, &scaleFactors)
	if header.Layer() == frame.LayerI {
		for group := 0; group < 3; group++ {
			dequantize(d.granules[0][group*4:], bitReader, &scaleFactors, 1)
		}
		applyScaleFactors(&scaleFactors, 2, d.granules[0][:])
	} else {
		for block := 0; block < blockCount; block++ {
			dequantize(d.granules[block][:], bitReader, &scaleFactors, 3)
			applyScaleFactors(&scaleFactors, block, d.granules[block][:])
		}
	}
	if bitReader.Overrun() {
		return io.ErrUnexpectedEOF
	}

	channelCount := channels
	if header.ChannelMode() == frame.Mono {
		channelCount = 1
	}
	for block := 0; block < blockCount; block++ {
		synthesize(d.granules[block][:channelCount*granuleSamples], block)
	}
	return nil
}
