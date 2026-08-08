package streaminfo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/godexture/godec/core/domain/media"
)

const (
	Marker = "fLaC"

	MetadataTypeStreamInfo    = 0
	MetadataTypeVorbisComment = 4
	MetadataTypePicture       = 6
	Length                    = 34

	MetadataKey = "flac.streaminfo"
	// MetadataBlockKey stores opaque FLAC metadata blocks. Each value contains
	// the four-byte FLAC block header followed by its payload.
	MetadataBlockKey = "flac.metadata.block"
	MaxChannels      = 8
)

// PCMMD5Parameters identifies a 16-byte source PCM MD5 payload.
type PCMMD5Parameters struct{}

type StreamInfo struct {
	MinBlockSize  uint16
	MaxBlockSize  uint16
	MinFrameSize  uint32
	MaxFrameSize  uint32
	SampleRate    int
	Channels      int
	BitsPerSample int
	TotalSamples  uint64
	MD5           [16]byte
}

// Duration returns the stream duration derived from TotalSamples.
// It returns 0 when the duration is unknown (TotalSamples or SampleRate is
// unset) or does not fit in time.Duration.
func (s StreamInfo) Duration() time.Duration {
	if s.TotalSamples == 0 || s.SampleRate <= 0 {
		return 0
	}

	rate := uint64(s.SampleRate)
	seconds := s.TotalSamples / rate
	if seconds >= uint64(math.MaxInt64/time.Second) {
		return 0
	}

	remainder := s.TotalSamples % rate
	return time.Duration(seconds)*time.Second + time.Duration(remainder*uint64(time.Second)/rate)
}

// Encode serializes a STREAMINFO metadata block payload.
func Encode(info StreamInfo) []byte {
	data := make([]byte, Length)
	binary.BigEndian.PutUint16(data[0:2], info.MinBlockSize)
	binary.BigEndian.PutUint16(data[2:4], info.MaxBlockSize)
	data[4] = byte(info.MinFrameSize >> 16)
	data[5] = byte(info.MinFrameSize >> 8)
	data[6] = byte(info.MinFrameSize)
	data[7] = byte(info.MaxFrameSize >> 16)
	data[8] = byte(info.MaxFrameSize >> 8)
	data[9] = byte(info.MaxFrameSize)
	data[10] = byte(info.SampleRate >> 12)
	data[11] = byte(info.SampleRate >> 4)
	data[12] = byte(info.SampleRate<<4) | byte((info.Channels-1)<<1) | byte((info.BitsPerSample-1)>>4)
	data[13] = byte((info.BitsPerSample-1)<<4) | byte(info.TotalSamples>>32)
	binary.BigEndian.PutUint32(data[14:18], uint32(info.TotalSamples))
	copy(data[18:34], info.MD5[:])
	return data
}

// ParseBlockHeader decodes a 4-byte FLAC metadata block header into its
// last-block flag, block type, and payload length.
func ParseBlockHeader(header [4]byte) (isLast bool, blockType byte, length int) {
	isLast = header[0]&0x80 != 0
	blockType = header[0] & 0x7f
	length = int(header[1])<<16 | int(header[2])<<8 | int(header[3])
	return isLast, blockType, length
}

// Parse decodes and validates a 34-byte FLAC STREAMINFO metadata block.
func Parse(data []byte) (StreamInfo, error) {
	if len(data) != Length {
		return StreamInfo{}, fmt.Errorf("invalid STREAMINFO length: %d", len(data))
	}
	info := StreamInfo{
		MinBlockSize:  binary.BigEndian.Uint16(data[0:2]),
		MaxBlockSize:  binary.BigEndian.Uint16(data[2:4]),
		MinFrameSize:  uint32(data[4])<<16 | uint32(data[5])<<8 | uint32(data[6]),
		MaxFrameSize:  uint32(data[7])<<16 | uint32(data[8])<<8 | uint32(data[9]),
		SampleRate:    int(data[10])<<12 | int(data[11])<<4 | int(data[12]>>4),
		Channels:      int((data[12]>>1)&0x07) + 1,
		BitsPerSample: int(((uint16(data[12])&0x01)<<4)|uint16(data[13]>>4)) + 1,
		TotalSamples:  (uint64(data[13]&0x0f) << 32) | uint64(binary.BigEndian.Uint32(data[14:18])),
	}
	copy(info.MD5[:], data[18:34])
	if err := Validate(info); err != nil {
		return StreamInfo{}, err
	}
	return info, nil
}

// Validate checks that a StreamInfo (whether parsed or synthesized) has
// sane values.
func Validate(info StreamInfo) error {
	if info.MinBlockSize < 16 || info.MaxBlockSize < 16 || info.MinBlockSize > info.MaxBlockSize {
		return errors.New("invalid FLAC block size in STREAMINFO")
	}
	if info.SampleRate <= 0 || info.SampleRate > 1048575 {
		return errors.New("invalid FLAC sample rate in STREAMINFO")
	}
	if info.Channels <= 0 || info.Channels > MaxChannels {
		return fmt.Errorf("invalid FLAC channel count: %d", info.Channels)
	}
	if info.BitsPerSample < 4 || info.BitsPerSample > 32 {
		return fmt.Errorf("unsupported FLAC bit depth: %d", info.BitsPerSample)
	}
	return nil
}

// SampleFormat maps a FLAC bit depth to the PCM sample format used to
// represent decoded samples.
func SampleFormat(bitsPerSample int) media.SampleFormat {
	if bitsPerSample <= 8 {
		return media.SampleFormatS8
	}
	if bitsPerSample <= 16 {
		return media.SampleFormatS16
	}
	if bitsPerSample <= 24 {
		return media.SampleFormatS24
	}
	return media.SampleFormatS32
}

// ChannelLayout maps a FLAC channel count to a channel layout.
func ChannelLayout(channels int) media.ChannelLayout {
	switch channels {
	case 1:
		return media.LayoutMono1
	case 2:
		return media.LayoutStereo2_0
	case 3:
		return media.LayoutStereo3_0
	case 4:
		return media.LayoutQuad4_0
	case 5:
		return media.LayoutFront5_0
	case 6:
		return media.LayoutFront5_1
	case 7:
		return media.LayoutSide6_1
	case 8:
		return media.LayoutWide7_1
	default:
		return media.NewUnspecified(channels)
	}
}
