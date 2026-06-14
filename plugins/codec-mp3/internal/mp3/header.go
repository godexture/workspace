package mp3

/*
#cgo CFLAGS: -O3 -DMINIMP3_FLOAT_OUTPUT
#include "minimp3.h"

int mp3dec_skip_id3_bytes(const unsigned char *buf, int size);
*/
import "C"
import (
	"unsafe"
)

// Header contains the parsed MPEG frame header information.
type Header struct {
	FrameBytes  int
	FrameOffset int
	Channels    int
	Hz          int
	Layer       int
	BitrateKbps int
}

// SkipId3 returns the number of bytes at the beginning of the buffer to skip (e.g. ID3 tags).
func SkipId3(mp3 []byte) int {
	if len(mp3) == 0 {
		return 0
	}
	return int(C.mp3dec_skip_id3_bytes((*C.uint8_t)(unsafe.Pointer(&mp3[0])), C.int(len(mp3))))
}

// BitReader is the Go equivalent of bs_t.
type BitReader struct {
	buf   []byte
	pos   int
	limit int
}

func (br *BitReader) Init(buf []byte) {
	br.buf = buf
	br.pos = 0
	br.limit = len(buf) * 8
}

func (br *BitReader) GetBits(n int) uint32 {
	s := br.pos & 7
	shl := n + s
	pIdx := br.pos >> 3
	br.pos += n
	if br.pos > br.limit {
		return 0
	}
	next := uint32(br.buf[pIdx]) & (255 >> s)
	pIdx++
	cache := uint32(0)
	for shl > 8 {
		shl -= 8
		cache |= next << shl
		next = uint32(br.buf[pIdx])
		pIdx++
	}
	shl -= 8
	return cache | (next >> -shl)
}

const hdrSize = 4

func hdrIsMono(h []byte) bool         { return (h[3] & 0xC0) == 0xC0 }
func hdrIsMsStereo(h []byte) bool     { return (h[3] & 0xE0) == 0x60 }
func hdrIsFreeFormat(h []byte) bool   { return (h[2] & 0xF0) == 0 }
func hdrIsCrc(h []byte) bool          { return (h[1] & 1) == 0 }
func hdrTestPadding(h []byte) bool    { return (h[2] & 0x2) != 0 }
func hdrTestMpeg1(h []byte) bool      { return (h[1] & 0x8) != 0 }
func hdrTestNotMpeg25(h []byte) bool  { return (h[1] & 0x10) != 0 }
func hdrTestIStereo(h []byte) bool    { return (h[3] & 0x10) != 0 }
func hdrTestMsStereo(h []byte) bool   { return (h[3] & 0x20) != 0 }
func hdrGetStereoMode(h []byte) int   { return int((h[3] >> 6) & 3) }
func hdrGetStereoModeExt(h []byte) int { return int((h[3] >> 4) & 3) }
func hdrGetLayer(h []byte) int        { return int((h[1] >> 1) & 3) }
func hdrGetBitrate(h []byte) int      { return int(h[2] >> 4) }
func hdrGetSampleRate(h []byte) int   { return int((h[2] >> 2) & 3) }
func hdrGetMySampleRate(h []byte) int {
	return hdrGetSampleRate(h) + (int((h[1]>>3)&1)+int((h[1]>>4)&1))*3
}
func hdrIsFrame576(h []byte) bool { return (h[1] & 14) == 2 }
func hdrIsLayer1(h []byte) bool   { return (h[1] & 6) == 6 }

func hdrBitrateKbps(h []byte) int {
	halfrate := [2][3][15]int{
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
	mpeg1Idx := 0
	if hdrTestMpeg1(h) {
		mpeg1Idx = 1
	}
	layerIdx := hdrGetLayer(h) - 1
	bitrateIdx := hdrGetBitrate(h)
	if layerIdx < 0 || layerIdx > 2 || bitrateIdx < 0 || bitrateIdx > 14 {
		return 0
	}
	return 2 * halfrate[mpeg1Idx][layerIdx][bitrateIdx]
}

func hdrSampleRateHz(h []byte) int {
	gHz := [3]int{44100, 48000, 32000}
	rateIdx := hdrGetSampleRate(h)
	if rateIdx < 0 || rateIdx > 2 {
		return 0
	}
	hz := gHz[rateIdx]
	if !hdrTestMpeg1(h) {
		hz >>= 1
	}
	if !hdrTestNotMpeg25(h) {
		hz >>= 1
	}
	return hz
}

func hdrFrameSamples(h []byte) int {
	if hdrIsLayer1(h) {
		return 384
	}
	shift := 0
	if hdrIsFrame576(h) {
		shift = 1
	}
	return 1152 >> shift
}

func hdrFrameBytes(h []byte, freeFormatSize int) int {
	samples := hdrFrameSamples(h)
	bitrate := hdrBitrateKbps(h)
	hz := hdrSampleRateHz(h)
	if hz == 0 {
		return 0
	}
	frameBytes := samples * bitrate * 125 / hz
	if hdrIsLayer1(h) {
		frameBytes &= ^3 // slot align
	}
	if frameBytes != 0 {
		return frameBytes
	}
	return freeFormatSize
}

func hdrPadding(h []byte) int {
	if hdrTestPadding(h) {
		if hdrIsLayer1(h) {
			return 4
		}
		return 1
	}
	return 0
}

func hdrValid(h []byte) bool {
	if len(h) < 2 {
		return false
	}
	return h[0] == 0xff &&
		((h[1]&0xf0) == 0xf0 || (h[1]&0xfe) == 0xe2) &&
		(hdrGetLayer(h) != 0) &&
		(hdrGetBitrate(h) != 15) &&
		(hdrGetSampleRate(h) != 3)
}

func hdrCompare(h1, h2 []byte) bool {
	if len(h1) < 4 || len(h2) < 4 {
		return false
	}
	return hdrValid(h2) &&
		((h1[1]^h2[1])&0xfe) == 0 &&
		((h1[2]^h2[2])&0x0c) == 0 &&
		hdrIsFreeFormat(h1) == hdrIsFreeFormat(h2)
}

const maxFreeFormatFrameSize = 2304
const maxFrameSyncMatches = 10
const maxBitreservoirBytes = 511
const maxL3FramePayloadBytes = 2304

func mp3dMatchFrame(hdr []byte, mp3Bytes int, frameBytes int) bool {
	i := 0
	nmatch := 0
	for ; nmatch < maxFrameSyncMatches; nmatch++ {
		i += hdrFrameBytes(hdr[i:], frameBytes) + hdrPadding(hdr[i:])
		if i+hdrSize > mp3Bytes {
			return nmatch > 0
		}
		if !hdrCompare(hdr, hdr[i:]) {
			return false
		}
	}
	return true
}

func mp3dFindFrame(mp3 []byte, mp3Bytes int, freeFormatBytes *int, ptrFrameBytes *int) int {
	i := 0
	for ; i < mp3Bytes-hdrSize; i++ {
		curr := mp3[i:]
		if hdrValid(curr) {
			frameBytes := hdrFrameBytes(curr, *freeFormatBytes)
			frameAndPadding := frameBytes + hdrPadding(curr)

			for k := hdrSize; frameBytes == 0 && k < maxFreeFormatFrameSize && i+2*k < mp3Bytes-hdrSize; k++ {
				if hdrCompare(curr, curr[k:]) {
					fb := k - hdrPadding(curr)
					nextfb := fb + hdrPadding(curr[k:])
					if i+k+nextfb+hdrSize > mp3Bytes || !hdrCompare(curr, curr[k+nextfb:]) {
						continue
					}
					frameAndPadding = k
					frameBytes = fb
					*freeFormatBytes = fb
				}
			}

			if (frameBytes != 0 && i+frameAndPadding <= mp3Bytes &&
				mp3dMatchFrame(curr, mp3Bytes-i, frameBytes)) ||
				(i == 0 && frameAndPadding == mp3Bytes) {
				*ptrFrameBytes = frameAndPadding
				return i
			}
			*freeFormatBytes = 0
		}
	}
	*ptrFrameBytes = 0
	return mp3Bytes
}
