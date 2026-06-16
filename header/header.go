package header

import "errors"

type Header [4]byte

const (
	SamplesPerFrameLayer1   = 384
	SamplesPerFrameLayer23  = 1152
	SamplesPerGranuleLayer3 = 576
	ChannelModeMono         = 3
	BytesPerSecMultiplier   = 125
	ID3v2HeaderSize         = 10
)

var ErrHeaderTooShort = errors.New("header buffer too short")

func ParseHeader(headerBytes []byte) (Header, error) {
	var header Header
	if len(headerBytes) < 4 {
		return header, ErrHeaderTooShort
	}
	copy(header[:], headerBytes[:4])
	return header, nil
}

func (h Header) IsValid() bool {
	return h[0] == 0xff &&
		((h[1]&0xf0) == 0xf0 || (h[1]&0xfe) == 0xe2) &&
		(h.Layer() != 0) &&
		(h.Bitrate() != 15) &&
		(h.SampleRate() != 3)
}

func (h Header) Compare(other Header) bool {
	return other.IsValid() &&
		((h[1]^other[1])&0xfe) == 0 &&
		((h[2]^other[2])&0x0c) == 0 &&
		h.IsFreeFormat() == other.IsFreeFormat()
}

func (h Header) IsMono() bool                   { return (h[3] & 0xC0) == 0xC0 }
func (h Header) IsMidSideStereo() bool          { return (h[3] & 0xE0) == 0x60 }
func (h Header) IsFreeFormat() bool             { return (h[2] & 0xF0) == 0 }
func (h Header) IsCyclicRedundancyCheck() bool  { return (h[1] & 1) == 0 }
func (h Header) HasPadding() bool               { return (h[2] & 0x2) != 0 }
func (h Header) IsMPEG1() bool                  { return (h[1] & 0x8) != 0 }
func (h Header) IsNotMPEG25() bool              { return (h[1] & 0x10) != 0 }
func (h Header) IsIntensityStereoEnabled() bool { return (h[3] & 0x10) != 0 }
func (h Header) IsMidSideStereoEnabled() bool   { return (h[3] & 0x20) != 0 }
func (h Header) StereoMode() int                { return int((h[3] >> 6) & 3) }
func (h Header) StereoModeExt() int             { return int((h[3] >> 4) & 3) }
func (h Header) Layer() int                     { return int((h[1] >> 1) & 3) }
func (h Header) Bitrate() int                   { return int(h[2] >> 4) }
func (h Header) SampleRate() int                { return int((h[2] >> 2) & 3) }
func (h Header) UnifiedSampleRateIndex() int {
	return h.SampleRate() + (int((h[1]>>3)&1)+int((h[1]>>4)&1))*3
}
func (h Header) VersionCode() int { return (int(h[1]) >> 3) & 0x03 }
func (h Header) IsFrame576() bool { return (h[1] & 14) == 2 }
func (h Header) IsLayer1() bool   { return (h[1] & 6) == 6 }

var halfBitrateTable = [2][3][15]int{
	{
		{0, 4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 72, 80},
		{0, 4, 8, 12, 16, 20, 24, 28, 32, 40, 48, 56, 64, 72, 80},
		{0, 16, 24, 28, 32, 40, 48, 56, 64, 72, 80, 88, 96, 112, 128},
	},
	{
		{0, 16, 20, 24, 28, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160},
		{0, 16, 24, 28, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192},
		{0, 16, 32, 48, 64, 80, 96, 112, 128, 144, 160, 176, 192, 208, 224},
	},
}

func (h Header) BitrateKbps() int {
	mpeg1Index := 0
	if h.IsMPEG1() {
		mpeg1Index = 1
	}
	layerIndex := h.Layer() - 1
	bitrateIndex := h.Bitrate()
	if layerIndex < 0 || layerIndex > 2 || bitrateIndex < 0 || bitrateIndex > 14 {
		return 0
	}
	return 2 * halfBitrateTable[mpeg1Index][layerIndex][bitrateIndex]
}

var sampleRateHzTableMPEG1 = [3]int{44100, 48000, 32000}

func (h Header) SampleRateHz() int {
	rateIndex := h.SampleRate()
	if rateIndex < 0 || rateIndex > 2 {
		return 0
	}
	sampleRateHertz := sampleRateHzTableMPEG1[rateIndex]
	if !h.IsMPEG1() {
		sampleRateHertz >>= 1
	}
	if !h.IsNotMPEG25() {
		sampleRateHertz >>= 1
	}
	return sampleRateHertz
}

func (h Header) FrameSamples() int {
	if h.IsLayer1() {
		return SamplesPerFrameLayer1
	}
	shift := 0
	if h.IsFrame576() {
		shift = 1
	}
	return SamplesPerFrameLayer23 >> shift
}

func (h Header) FrameBytes(freeFormatSize int) int {
	samples := h.FrameSamples()
	bitrate := h.BitrateKbps()
	sampleRateHertz := h.SampleRateHz()
	if sampleRateHertz == 0 {
		return 0
	}
	frameBytes := samples * bitrate * BytesPerSecMultiplier / sampleRateHertz
	if h.IsLayer1() {
		frameBytes &= ^3 // slot align
	}
	if frameBytes != 0 {
		return frameBytes
	}
	return freeFormatSize
}

func (h Header) Padding() int {
	if h.HasPadding() {
		if h.IsLayer1() {
			return 4
		}
		return 1
	}
	return 0
}
