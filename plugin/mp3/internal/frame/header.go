// Package frame parses and locates MPEG audio frames.
package frame

import "errors"

var (
	// ErrHeaderTooShort indicates that fewer than four header bytes were supplied.
	ErrHeaderTooShort = errors.New("mp3 frame header is too short")
	// ErrInvalidHeader indicates a sync, version, layer, bitrate, sample-rate, or
	// emphasis field that is not permitted by the MPEG audio header format.
	ErrInvalidHeader = errors.New("invalid mp3 frame header")
	// ErrFreeFormatSize indicates that a free-format frame needs a size hint or
	// repeated headers before its size can be derived.
	ErrFreeFormatSize = errors.New("free-format frame size is unavailable")
	// ErrFrameSize indicates that a supplied free-format size cannot describe a
	// frame.
	ErrFrameSize = errors.New("invalid mp3 frame size")
)

// Version identifies the MPEG audio version in a frame header.
type Version uint8

const (
	VersionMPEG25 Version = iota
	VersionMPEG2
	VersionMPEG1
)

func (v Version) String() string {
	switch v {
	case VersionMPEG25:
		return "MPEG-2.5"
	case VersionMPEG2:
		return "MPEG-2"
	case VersionMPEG1:
		return "MPEG-1"
	default:
		return "reserved"
	}
}

// Layer identifies the MPEG audio layer.
type Layer uint8

const (
	LayerI   Layer = 1
	LayerII  Layer = 2
	LayerIII Layer = 3
)

// ChannelMode identifies how the two channel elements are arranged.
type ChannelMode uint8

const (
	Stereo      ChannelMode = 0
	JointStereo ChannelMode = 1
	DualChannel ChannelMode = 2
	Mono        ChannelMode = 3
)

// Header is a validated four-byte MPEG audio frame header.
//
// A Header is created with Parse. Its derived values are kept with the raw
// bytes so callers do not need to repeat bit-field decoding while scanning.
type Header struct {
	raw             [4]byte
	valid           bool
	version         Version
	layer           Layer
	bitrateIndex    byte
	bitrateKbps     int
	sampleRateIndex byte
	sampleRateHz    int
	samples         int
	padding         bool
	channelMode     ChannelMode
}

// Parse parses and validates the first four bytes of data.
func Parse(data []byte) (Header, error) {
	if len(data) < 4 {
		return Header{}, ErrHeaderTooShort
	}

	var raw [4]byte
	copy(raw[:], data[:4])
	return parseRaw(raw)
}

func parseRaw(raw [4]byte) (Header, error) {
	// The sync word occupies all eight bits of byte 0 and the top three bits of
	// byte 1. The remaining five bits are version, layer, and protection.
	if raw[0] != 0xff || raw[1]&0xe0 != 0xe0 {
		return Header{}, ErrInvalidHeader
	}

	var version Version
	switch (raw[1] >> 3) & 0x03 {
	case 0:
		version = VersionMPEG25
	case 2:
		version = VersionMPEG2
	case 3:
		version = VersionMPEG1
	default:
		return Header{}, ErrInvalidHeader
	}

	var layer Layer
	switch (raw[1] >> 1) & 0x03 {
	case 1:
		layer = LayerIII
	case 2:
		layer = LayerII
	case 3:
		layer = LayerI
	default:
		return Header{}, ErrInvalidHeader
	}

	bitrateIndex := raw[2] >> 4
	if bitrateIndex == 15 {
		return Header{}, ErrInvalidHeader
	}

	sampleRateIndex := (raw[2] >> 2) & 0x03
	if sampleRateIndex == 3 {
		return Header{}, ErrInvalidHeader
	}

	// Emphasis 10 is reserved in all MPEG audio versions.
	if raw[3]&0x03 == 2 {
		return Header{}, ErrInvalidHeader
	}

	bitrate := bitrate(version, layer, bitrateIndex)
	sampleRate := sampleRate(version, sampleRateIndex)
	samples := samplesPerFrame(version, layer)
	if sampleRate == 0 || samples == 0 || (bitrateIndex != 0 && bitrate == 0) {
		return Header{}, ErrInvalidHeader
	}

	return Header{
		raw:             raw,
		valid:           true,
		version:         version,
		layer:           layer,
		bitrateIndex:    bitrateIndex,
		bitrateKbps:     bitrate,
		sampleRateIndex: sampleRateIndex,
		sampleRateHz:    sampleRate,
		samples:         samples,
		padding:         raw[2]&0x02 != 0,
		channelMode:     ChannelMode(raw[3] >> 6),
	}, nil
}

// Valid reports whether h was parsed successfully.
func (h Header) Valid() bool { return h.valid }

// Bytes returns the original four header bytes.
func (h Header) Bytes() [4]byte { return h.raw }

// Version returns the MPEG audio version.
func (h Header) Version() Version { return h.version }

// Layer returns the MPEG audio layer.
func (h Header) Layer() Layer { return h.layer }

// BitrateKbps returns the bitrate represented by the header, or zero for a
// free-format header.
func (h Header) BitrateKbps() int { return h.bitrateKbps }

// SampleRateHz returns the sample rate represented by the header.
func (h Header) SampleRateHz() int { return h.sampleRateHz }

// SamplesPerFrame returns the number of samples represented by one frame.
func (h Header) SamplesPerFrame() int { return h.samples }

// FreeFormat reports whether the header leaves the bitrate and frame size to
// the stream rather than selecting a table bitrate.
func (h Header) FreeFormat() bool { return h.valid && h.bitrateIndex == 0 }

// HasPadding reports whether this frame includes its version/layer padding
// slot.
func (h Header) HasPadding() bool { return h.padding }

// PaddingBytes returns the number of padding bytes in this frame.
func (h Header) PaddingBytes() int {
	if !h.padding {
		return 0
	}
	if h.layer == LayerI {
		return 4
	}
	return 1
}

// HasCRC reports whether a CRC follows this header.
func (h Header) HasCRC() bool { return h.valid && h.raw[1]&0x01 == 0 }

// ChannelMode returns the channel arrangement from the header.
func (h Header) ChannelMode() ChannelMode { return h.channelMode }

// FrameSize derives the total encoded frame size in bytes. For free-format
// frames, freeFormatBytes is the unpadded frame size and must be supplied.
func (h Header) FrameSize(freeFormatBytes int) (int, error) {
	if !h.valid {
		return 0, ErrInvalidHeader
	}
	if h.FreeFormat() {
		if freeFormatBytes < 4 {
			if freeFormatBytes == 0 {
				return 0, ErrFreeFormatSize
			}
			return 0, ErrFrameSize
		}
		return freeFormatBytes + h.PaddingBytes(), nil
	}

	coefficient := 144
	if h.layer == LayerI {
		coefficient = 12
	} else if h.layer == LayerIII && h.version != VersionMPEG1 {
		coefficient = 72
	}

	base := coefficient * h.bitrateKbps * 1000 / h.sampleRateHz
	if base < 1 {
		return 0, ErrFrameSize
	}
	if h.layer == LayerI {
		return (base + boolInt(h.padding)) * 4, nil
	}
	return base + boolInt(h.padding), nil
}

// Compatible reports whether two headers describe the same stream framing.
// Padding, bitrate, channel mode, and ancillary flags may vary between frames,
// but version, layer, sample rate, and bitrate mode must agree.
func (h Header) Compatible(other Header) bool {
	return h.valid && other.valid &&
		h.version == other.version &&
		h.layer == other.layer &&
		h.sampleRateHz == other.sampleRateHz &&
		h.FreeFormat() == other.FreeFormat()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func bitrate(version Version, layer Layer, index byte) int {
	tables := bitrateMPEG2
	if version == VersionMPEG1 {
		tables = bitrateMPEG1
	}
	return tables[layer-1][index]
}

func sampleRate(version Version, index byte) int {
	rate := sampleRates[index]
	switch version {
	case VersionMPEG2:
		rate /= 2
	case VersionMPEG25:
		rate /= 4
	}
	return rate
}

func samplesPerFrame(version Version, layer Layer) int {
	if layer == LayerI || layer == LayerII {
		if layer == LayerI {
			return 384
		}
		return 1152
	}
	if version == VersionMPEG1 {
		return 1152
	}
	return 576
}

var sampleRates = [...]int{44100, 48000, 32000}

var bitrateMPEG1 = [...][15]int{
	{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
	{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
	{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
}

var bitrateMPEG2 = [...][15]int{
	{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
}
