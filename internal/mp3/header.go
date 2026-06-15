package mp3

import "github.com/godexture/format-mp3/header"

// ParseId3v2Size returns the total size of an ID3v2 tag (including header) if the buffer contains a valid ID3v2 header.
// The buffer must be at least 10 bytes long.
func ParseId3v2Size(buf []byte) (int, bool) {
	if len(buf) < 10 {
		return 0, false
	}
	if buf[0] == 'I' && buf[1] == 'D' && buf[2] == '3' &&
		!((buf[5]&15) != 0 || (buf[6]&0x80) != 0 || (buf[7]&0x80) != 0 || (buf[8]&0x80) != 0 || (buf[9]&0x80) != 0) {
		id3v2size := (int(buf[6]&0x7f) << 21) | (int(buf[7]&0x7f) << 14) | (int(buf[8]&0x7f) << 7) | int(buf[9]&0x7f)
		id3v2size += 10 // header
		if (buf[5] & 16) != 0 {
			id3v2size += 10 // footer
		}
		return id3v2size, true
	}
	return 0, false
}

// SkipId3 returns the number of bytes at the beginning of the buffer to skip (e.g. ID3 tags).
func SkipId3(mp3 []byte) int {
	if size, ok := ParseId3v2Size(mp3); ok {
		if size >= len(mp3) {
			return len(mp3)
		}
		return size
	}
	return 0
}

type Header = header.Header

var ParseHeader = header.ParseHeader

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
