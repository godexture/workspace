package mp3

import (
	"github.com/godexture/codec-mp3/internal/mp3/bits"
	"github.com/godexture/codec-mp3/internal/mp3/domain"
	"github.com/godexture/codec-mp3/internal/mp3/layer12"
	"github.com/godexture/codec-mp3/internal/mp3/layer3"

	"github.com/godexture/format-mp3/header"
)

const (
	MaxChannels              = header.MaxChannels
	QMFHistoryBlocks         = 15
	SubBandCount             = layer3.SubBandCount
	SamplesPerSubBandLayer3  = layer3.SamplesPerSubBand
	SamplesPerSubBandLayer12 = layer12.SamplesPerSubBand
)

type Decoder struct {
	QuadratureMirrorFilterState [QMFHistoryBlocks * MaxChannels * SubBandCount]float32
	FreeFormatBytes             int
	Header                      Header
	layer3Dec                   layer3.Decoder
	layer3Work                  layer3.Workspace
	synthesisWorkspace          [2112]float32
}

// Init initializes the decoder.
func (d *Decoder) Init() {
	*d = Decoder{}
	d.layer3Dec.Init()
}

// DecodeFrame decodes one MP3 frame to float32 samples.
// pcmSamples slice must be pre-allocated to hold up to SamplesPerFrameLayer23 * 2 samples.
// Returns the number of samples decoded *per channel*, the frame info, and any error encountered.
func (d *Decoder) DecodeFrame(mp3Data []byte, pcmSamples []float32) (int, domain.FrameInfo, error) {
	mp3DataLength := len(mp3Data)
	frameSize := 0
	byteIndex := 0

	if mp3DataLength > 4 && d.Header[0] == 0xff {
		nextHeader, err := ParseHeader(mp3Data)
		if err == nil && d.Header.Compare(nextHeader) {
			frameSize = nextHeader.FrameBytes(d.FreeFormatBytes) + nextHeader.Padding()
			if frameSize != mp3DataLength {
				if frameSize+4 > mp3DataLength {
					frameSize = 0
				} else {
					nextHeader2, err2 := ParseHeader(mp3Data[frameSize:])
					if err2 != nil || !nextHeader.Compare(nextHeader2) {
						frameSize = 0
					}
				}
			}
		}
	}

	if frameSize == 0 {
		d.Init()
		var frameFound bool
		byteIndex, frameSize, d.FreeFormatBytes, frameFound = FindFrame(mp3Data, d.FreeFormatBytes)
		if !frameFound || byteIndex+frameSize > mp3DataLength {
			return 0, domain.FrameInfo{FrameBytes: byteIndex}, nil
		}
	}

	header, err := ParseHeader(mp3Data[byteIndex : byteIndex+4])
	if err != nil {
		return 0, domain.FrameInfo{}, nil
	}
	d.Header = header

	frameInfo := domain.FrameInfo{
		FrameBytes:               byteIndex + frameSize,
		FrameOffset:              byteIndex,
		Channels:                 MaxChannels,
		SampleRateHertz:          header.SampleRateHz(),
		MpegLayer:                4 - header.Layer(),
		BitRateKilobitsPerSecond: header.BitrateKbps(),
	}
	if header.IsMono() {
		frameInfo.Channels = 1
	}

	if pcmSamples == nil {
		return header.FrameSamples(), frameInfo, nil
	}

	var bitStreamFrame bits.BitReader
	bitStreamFrame.Buffer = mp3Data[byteIndex+4:]
	bitStreamFrame.Position = 0
	bitStreamFrame.Limit = int32((frameSize - 4) * 8)

	if header.IsCyclicRedundancyCheck() {
		bitStreamFrame.GetBits(16)
	}

	if frameInfo.MpegLayer == 3 {
		err = d.decodeLayer3(frameInfo, &bitStreamFrame, pcmSamples, header)
	} else {
		err = d.decodeLayer12(frameInfo, &bitStreamFrame, pcmSamples, header)
	}

	if err != nil {
		return 0, frameInfo, err
	}
	return d.Header.FrameSamples(), frameInfo, nil
}

func (d *Decoder) decodeLayer3(frameInfo domain.FrameInfo, bitStreamFrame *bits.BitReader, pcmSamples []float32, h Header) error {
	synthesize := func(granule []float32, pcmOffset int) {
		SynthesizeGranule(d.QuadratureMirrorFilterState[:], granule, SamplesPerSubBandLayer3, frameInfo.Channels, pcmSamples[pcmOffset:], d.synthesisWorkspace[:])
	}
	return layer3.Decode(&d.layer3Dec, &d.layer3Work, bitStreamFrame, frameInfo.Channels, h, synthesize)
}

func (d *Decoder) decodeLayer12(frameInfo domain.FrameInfo, bitStreamFrame *bits.BitReader, pcmSamples []float32, h Header) error {
	synthesize := func(granule []float32, pcmOffset int) {
		SynthesizeGranule(d.QuadratureMirrorFilterState[:], granule, SamplesPerSubBandLayer12, frameInfo.Channels, pcmSamples[pcmOffset:], d.synthesisWorkspace[:])
	}
	return layer12.Decode(bitStreamFrame, frameInfo.Channels, frameInfo.MpegLayer, h, synthesize)
}
