package layer12

import (
	"github.com/godexture/godec/plugin/mp3/header"
	"github.com/godexture/godec/plugin/mp3/internal/codec/mp3/domain"
	"github.com/godexture/godec/sdk/bits"
)

const (
	stereoModeJointStereo  = 1
	stereoModeMono         = 3
	synthesisBufferStride  = header.SamplesPerSubBandLayer3
	synthesisChannelOffset = header.SamplesPerGranuleLayer3
	maxGranuleBufferSize   = header.SamplesPerGranuleLayer3 * header.MaxChannels
)

const (
	SamplesPerSubBand = header.SamplesPerSubBandLayer12
)

func Decode(
	bitStreamFrame *bits.Reader,
	channels int,
	mpegLayer int,
	h header.Header,
	synthesize func(granule []float32, pcmOffset int),
) error {
	var scaleFactorInfo ScaleFactorInfo
	ReadScaleFactorInfo(h, bitStreamFrame, &scaleFactorInfo)

	var granule [maxGranuleBufferSize]float32
	iVal := 0
	pcmOffset := 0
	granuleFlat := granule[:]
	for granuleIndex := 0; granuleIndex < 3; granuleIndex++ {
		dequantizedSamplesCount := DequantizeGranule(granuleFlat[iVal:], bitStreamFrame, &scaleFactorInfo, mpegLayer|1)
		iVal += dequantizedSamplesCount
		if iVal == SamplesPerSubBand {
			iVal = 0
			ApplyScaleFactors384(&scaleFactorInfo, scaleFactorInfo.ScaleFactors[granuleIndex:], granuleFlat)
			synthesize(granuleFlat, pcmOffset)
			granule = [maxGranuleBufferSize]float32{}
			pcmOffset += header.SamplesPerFrameLayer1 * channels
		}
		if bitStreamFrame.Overrun() {
			return domain.ErrBitStreamUnderflow
		}
	}
	return nil
}
