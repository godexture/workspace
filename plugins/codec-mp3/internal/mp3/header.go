package mp3

import "github.com/godexture/format-mp3/header"

// ParseID3v2Size returns the total size of an ID3v2 tag (including header) if the headerBytes contains a valid ID3v2 header.
// The headerBytes must be at least 10 bytes long.
func ParseID3v2Size(buffer []byte) (int, bool) {
	if len(buffer) < ID3v2HeaderSize {
		return 0, false
	}
	if buffer[0] == 'I' && buffer[1] == 'D' && buffer[2] == '3' &&
		!((buffer[5]&15) != 0 || (buffer[6]&0x80) != 0 || (buffer[7]&0x80) != 0 || (buffer[8]&0x80) != 0 || (buffer[9]&0x80) != 0) {
		tagSize := (int(buffer[6]&0x7f) << 21) | (int(buffer[7]&0x7f) << 14) | (int(buffer[8]&0x7f) << 7) | int(buffer[9]&0x7f)
		tagSize += ID3v2HeaderSize // header
		if (buffer[5] & 16) != 0 {
			tagSize += ID3v2HeaderSize // footer
		}
		return tagSize, true
	}
	return 0, false
}

// SkipID3 returns the number of bytes at the beginning of the buffer to skip (e.g. ID3 tags).
func SkipID3(buffer []byte) int {
	if size, ok := ParseID3v2Size(buffer); ok {
		if size >= len(buffer) {
			return len(buffer)
		}
		return size
	}
	return 0
}

type Header = header.Header

var ParseHeader = header.ParseHeader

const (
	SamplesPerFrameLayer1  = header.SamplesPerFrameLayer1
	SamplesPerFrameLayer23 = header.SamplesPerFrameLayer23
	ChannelModeMono        = header.ChannelModeMono
	BytesPerSecMultiplier  = header.BytesPerSecMultiplier
	ID3v2HeaderSize        = header.ID3v2HeaderSize
)

const (
	MaxFreeFormatFrameSize = 2304
	MaxFrameSyncMatches    = 10
)

func matchFrame(mp3Data []byte, header Header, freeFormatBytes int) bool {
	byteIndex := 0
	matchCount := 0
	currentHeader := header
	for ; matchCount < MaxFrameSyncMatches; matchCount++ {
		byteIndex += currentHeader.FrameBytes(freeFormatBytes) + currentHeader.Padding()
		if byteIndex+4 > len(mp3Data) {
			return matchCount > 0
		}
		nextHeader, err := ParseHeader(mp3Data[byteIndex : byteIndex+4])
		if err != nil || !header.Compare(nextHeader) {
			return false
		}
		currentHeader = nextHeader
	}
	return true
}

func FindFrame(mp3Data []byte, freeFormatBytes int) (offset int, frameBytes int, newFreeFormatBytes int, found bool) {
	mp3DataLength := len(mp3Data)
	for byteIndex := 0; byteIndex < mp3DataLength-4; byteIndex++ {
		currentHeader, err := ParseHeader(mp3Data[byteIndex : byteIndex+4])
		if err == nil && currentHeader.IsValid() {
			frameBytes := currentHeader.FrameBytes(freeFormatBytes)
			frameAndPadding := frameBytes + currentHeader.Padding()

			for offsetStep := 4; frameBytes == 0 && offsetStep < MaxFreeFormatFrameSize && byteIndex+2*offsetStep < mp3DataLength-4; offsetStep++ {
				nextHeader, err2 := ParseHeader(mp3Data[byteIndex+offsetStep : byteIndex+offsetStep+4])
				if err2 == nil && currentHeader.Compare(nextHeader) {
					foundFrameBytes := offsetStep - currentHeader.Padding()
					nextFrameBytes := foundFrameBytes + nextHeader.Padding()
					if byteIndex+offsetStep+nextFrameBytes+4 > mp3DataLength {
						continue
					}
					nextHeader2, err3 := ParseHeader(mp3Data[byteIndex+offsetStep+nextFrameBytes : byteIndex+offsetStep+nextFrameBytes+4])
					if err3 != nil || !currentHeader.Compare(nextHeader2) {
						continue
					}
					frameAndPadding = offsetStep
					frameBytes = foundFrameBytes
					freeFormatBytes = foundFrameBytes
				}
			}

			if (frameBytes != 0 && byteIndex+frameAndPadding <= mp3DataLength &&
				matchFrame(mp3Data[byteIndex:], currentHeader, frameBytes)) ||
				(byteIndex == 0 && frameAndPadding == mp3DataLength) {
				return byteIndex, frameAndPadding, freeFormatBytes, true
			}
			freeFormatBytes = 0
		}
	}
	return mp3DataLength, 0, 0, false
}
