// Package frame implements the FLAC frame wire format.
package frame

import (
	"errors"
	"io"

	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

type Header struct {
	BlockSize         int
	SampleRate        int
	Channels          int
	ChannelAssignment uint8
	BitsPerSample     int
	BlockingStrategy  bool
	Number            uint64
	HeaderBytes       int
	HeaderCRC         byte
	FrameBytes        int
}

func ParseHeader(data []byte, info streaminfo.StreamInfo) (Header, error) {
	r := bits.New(data)
	header, err := decodeHeader(r, info)
	if err != nil {
		return Header{}, err
	}
	if header.HeaderBytes > len(data) {
		return Header{}, io.ErrUnexpectedEOF
	}
	if header.HeaderBytes < 1 || hash.CRC8(data[:header.HeaderBytes-1]) != header.HeaderCRC {
		return Header{}, errors.New("invalid FLAC frame header CRC-8")
	}
	return header, nil
}
