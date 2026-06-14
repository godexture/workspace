package mp3

// SkipId3 returns the number of bytes at the beginning of the buffer to skip (e.g. ID3 tags).
func SkipId3(mp3 []byte) int {
	if len(mp3) == 0 {
		return 0
	}
	buf := mp3
	bufSize := len(mp3)

	// Check ID3v2
	id3v2size := 0
	if bufSize >= 10 && buf[0] == 'I' && buf[1] == 'D' && buf[2] == '3' &&
		!((buf[5]&15) != 0 || (buf[6]&0x80) != 0 || (buf[7]&0x80) != 0 || (buf[8]&0x80) != 0 || (buf[9]&0x80) != 0) {
		id3v2size = (int(buf[6]&0x7f) << 21) | (int(buf[7]&0x7f) << 14) | (int(buf[8]&0x7f) << 7) | int(buf[9]&0x7f)
		id3v2size += 10 // header
		if (buf[5] & 16) != 0 {
			id3v2size += 10 // footer
		}
	}

	if id3v2size > 0 {
		if id3v2size >= bufSize {
			return bufSize
		}
		return id3v2size
	}
	return 0
}

type Header [4]byte

func ParseHeader(b []byte) (Header, bool) {
	var h Header
	if len(b) < 4 {
		return h, false
	}
	copy(h[:], b[:4])
	return h, true
}

func (h Header) IsMono() bool        { return (h[3] & 0xC0) == 0xC0 }
func (h Header) IsMsStereo() bool    { return (h[3] & 0xE0) == 0x60 }
func (h Header) IsFreeFormat() bool  { return (h[2] & 0xF0) == 0 }
func (h Header) IsCrc() bool         { return (h[1] & 1) == 0 }
func (h Header) TestPadding() bool   { return (h[2] & 0x2) != 0 }
func (h Header) TestMpeg1() bool     { return (h[1] & 0x8) != 0 }
func (h Header) TestNotMpeg25() bool { return (h[1] & 0x10) != 0 }
func (h Header) TestIStereo() bool   { return (h[3] & 0x10) != 0 }
func (h Header) TestMsStereo() bool  { return (h[3] & 0x20) != 0 }
func (h Header) StereoMode() int     { return int((h[3] >> 6) & 3) }
func (h Header) StereoModeExt() int  { return int((h[3] >> 4) & 3) }
func (h Header) Layer() int          { return int((h[1] >> 1) & 3) }
func (h Header) Bitrate() int        { return int(h[2] >> 4) }
func (h Header) SampleRate() int     { return int((h[2] >> 2) & 3) }
func (h Header) MySampleRate() int {
	return h.SampleRate() + (int((h[1]>>3)&1)+int((h[1]>>4)&1))*3
}
func (h Header) IsFrame576() bool { return (h[1] & 14) == 2 }
func (h Header) IsLayer1() bool   { return (h[1] & 6) == 6 }

var halfrate = [2][3][15]int{
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
	mpeg1Idx := 0
	if h.TestMpeg1() {
		mpeg1Idx = 1
	}
	layerIdx := h.Layer() - 1
	bitrateIdx := h.Bitrate()
	if layerIdx < 0 || layerIdx > 2 || bitrateIdx < 0 || bitrateIdx > 14 {
		return 0
	}
	return 2 * halfrate[mpeg1Idx][layerIdx][bitrateIdx]
}

var hzTable = [3]int{44100, 48000, 32000}

func (h Header) SampleRateHz() int {
	rateIdx := h.SampleRate()
	if rateIdx < 0 || rateIdx > 2 {
		return 0
	}
	hz := hzTable[rateIdx]
	if !h.TestMpeg1() {
		hz >>= 1
	}
	if !h.TestNotMpeg25() {
		hz >>= 1
	}
	return hz
}

func (h Header) FrameSamples() int {
	if h.IsLayer1() {
		return 384
	}
	shift := 0
	if h.IsFrame576() {
		shift = 1
	}
	return 1152 >> shift
}

func (h Header) FrameBytes(freeFormatSize int) int {
	samples := h.FrameSamples()
	bitrate := h.BitrateKbps()
	hz := h.SampleRateHz()
	if hz == 0 {
		return 0
	}
	frameBytes := samples * bitrate * 125 / hz
	if h.IsLayer1() {
		frameBytes &= ^3 // slot align
	}
	if frameBytes != 0 {
		return frameBytes
	}
	return freeFormatSize
}

func (h Header) Padding() int {
	if h.TestPadding() {
		if h.IsLayer1() {
			return 4
		}
		return 1
	}
	return 0
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

const maxFreeFormatFrameSize = 2304
const maxFrameSyncMatches = 10
const maxBitreservoirBytes = 511
const maxL3FramePayloadBytes = 2304

func matchFrame(mp3 []byte, header Header, freeFormatBytes int) bool {
	i := 0
	nmatch := 0
	currHeader := header
	for ; nmatch < maxFrameSyncMatches; nmatch++ {
		i += currHeader.FrameBytes(freeFormatBytes) + currHeader.Padding()
		if i+4 > len(mp3) {
			return nmatch > 0
		}
		nextHdr, ok := ParseHeader(mp3[i : i+4])
		if !ok || !header.Compare(nextHdr) {
			return false
		}
		currHeader = nextHdr
	}
	return true
}

func FindFrame(mp3 []byte, freeFormatBytes int) (offset int, frameBytes int, newFreeFormatBytes int, found bool) {
	mp3Bytes := len(mp3)
	for i := 0; i < mp3Bytes-4; i++ {
		curr, ok := ParseHeader(mp3[i : i+4])
		if ok && curr.IsValid() {
			frameBytes := curr.FrameBytes(freeFormatBytes)
			frameAndPadding := frameBytes + curr.Padding()

			for k := 4; frameBytes == 0 && k < maxFreeFormatFrameSize && i+2*k < mp3Bytes-4; k++ {
				nextHdr, ok2 := ParseHeader(mp3[i+k : i+k+4])
				if ok2 && curr.Compare(nextHdr) {
					fb := k - curr.Padding()
					nextfb := fb + nextHdr.Padding()
					if i+k+nextfb+4 > mp3Bytes {
						continue
					}
					nextHdr2, ok3 := ParseHeader(mp3[i+k+nextfb : i+k+nextfb+4])
					if !ok3 || !curr.Compare(nextHdr2) {
						continue
					}
					frameAndPadding = k
					frameBytes = fb
					freeFormatBytes = fb
				}
			}

			if (frameBytes != 0 && i+frameAndPadding <= mp3Bytes &&
				matchFrame(mp3[i:], curr, frameBytes)) ||
				(i == 0 && frameAndPadding == mp3Bytes) {
				return i, frameAndPadding, freeFormatBytes, true
			}
			freeFormatBytes = 0
		}
	}
	return mp3Bytes, 0, 0, false
}
