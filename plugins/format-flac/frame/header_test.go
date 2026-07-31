package frame

import (
	"testing"

	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/hash"
)

func TestParseHeaderRejectsBlockSize65536(t *testing.T) {
	t.Parallel()
	data := []byte{0xff, 0xf8, 0x79, 0x08, 0x00, 0xff, 0xff}
	data = append(data, hash.CRC8(data))

	_, err := ParseHeader(data, streaminfo.StreamInfo{SampleRate: 44100, Channels: 1, BitsPerSample: 16})
	if err == nil {
		t.Fatal("ParseHeader() accepted a 65536-sample block")
	}
}
