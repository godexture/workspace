package mp3

import "github.com/godexture/godec/plugin/mp3/header"

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
