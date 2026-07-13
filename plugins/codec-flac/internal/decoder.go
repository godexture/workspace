package internal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/engine"
)

const (
	flacMarker = "fLaC"

	metadataTypeStreamInfo = 0
	streamInfoLength       = 34

	maxFLACChannels = 8
)

type DecoderConfig struct{}

func (DecoderConfig) NodeConfiguration() {}

func DefaultDecoderConfig() DecoderConfig { return DecoderConfig{} }

type Decoder struct {
	config DecoderConfig

	buffer  []byte
	parsed  bool
	info    streamInfo
	flushed bool
}

type streamInfo struct {
	minBlockSize  uint16
	maxBlockSize  uint16
	minFrameSize  uint32
	maxFrameSize  uint32
	sampleRate    int
	channels      int
	bitsPerSample int
	totalSamples  uint64
}

type frameHeader struct {
	blockSize         int
	sampleRate        int
	channels          int
	channelAssignment uint8
	bitsPerSample     int
}

type decodedFrame struct {
	header  frameHeader
	samples [][]int32
	bytes   int
}

func NewDecoder(config DecoderConfig) *Decoder {
	return &Decoder{config: config}
}

func (d *Decoder) SendPacket(pkt *media.Packet) error {
	if pkt == nil {
		return errors.New("flac decoder requires a non-nil packet")
	}
	if d.flushed {
		return engine.ErrEOF
	}
	d.buffer = append(d.buffer, pkt.Data()...)
	return nil
}

func (d *Decoder) ReceiveFrame() (*media.Frame, error) {
	if len(d.buffer) == 0 {
		if d.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	if !d.parsed {
		consumed, err := d.parseStreamHeader()
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				if d.flushed {
					return nil, err
				}
				return nil, engine.ErrEAGAIN
			}
			return nil, err
		}
		d.buffer = d.buffer[consumed:]
		d.parsed = true
	}

	if len(d.buffer) == 0 {
		if d.flushed {
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	decoded, err := decodeFLACFrame(d.buffer, d.info)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			if d.flushed {
				return nil, err
			}
			return nil, engine.ErrEAGAIN
		}
		return nil, err
	}
	d.buffer = d.buffer[decoded.bytes:]

	audioFrame, err := buildAudioFrame(decoded)
	if err != nil {
		return nil, err
	}
	var frame media.Frame = audioFrame
	return &frame, nil
}

func (d *Decoder) Flush() error {
	d.flushed = true
	return nil
}

func (d *Decoder) parseStreamHeader() (int, error) {
	if len(d.buffer) < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	if string(d.buffer[:4]) != flacMarker {
		return 0, errors.New("not a native FLAC stream")
	}

	offset := 4
	seenStreamInfo := false
	for {
		if len(d.buffer)-offset < 4 {
			return 0, io.ErrUnexpectedEOF
		}
		header := d.buffer[offset]
		isLast := header&0x80 != 0
		blockType := header & 0x7f
		length := int(d.buffer[offset+1])<<16 | int(d.buffer[offset+2])<<8 | int(d.buffer[offset+3])
		offset += 4
		if len(d.buffer)-offset < length {
			return 0, io.ErrUnexpectedEOF
		}

		if blockType == metadataTypeStreamInfo {
			if seenStreamInfo {
				return 0, errors.New("duplicate FLAC STREAMINFO block")
			}
			if length != streamInfoLength {
				return 0, fmt.Errorf("invalid FLAC STREAMINFO length: %d", length)
			}
			info, err := parseStreamInfo(d.buffer[offset : offset+length])
			if err != nil {
				return 0, err
			}
			d.info = info
			seenStreamInfo = true
		} else if !seenStreamInfo {
			return 0, errors.New("FLAC STREAMINFO must be the first metadata block")
		}

		offset += length
		if isLast {
			break
		}
	}

	if !seenStreamInfo {
		return 0, errors.New("missing FLAC STREAMINFO block")
	}
	return offset, nil
}

func parseStreamInfo(data []byte) (streamInfo, error) {
	if len(data) != streamInfoLength {
		return streamInfo{}, fmt.Errorf("invalid STREAMINFO length: %d", len(data))
	}
	info := streamInfo{
		minBlockSize:  binary.BigEndian.Uint16(data[0:2]),
		maxBlockSize:  binary.BigEndian.Uint16(data[2:4]),
		minFrameSize:  uint32(data[4])<<16 | uint32(data[5])<<8 | uint32(data[6]),
		maxFrameSize:  uint32(data[7])<<16 | uint32(data[8])<<8 | uint32(data[9]),
		sampleRate:    int(data[10])<<12 | int(data[11])<<4 | int(data[12]>>4),
		channels:      int((data[12]>>1)&0x07) + 1,
		bitsPerSample: int(((uint16(data[12])&0x01)<<4)|uint16(data[13]>>4)) + 1,
		totalSamples:  (uint64(data[13]&0x0f) << 32) | uint64(binary.BigEndian.Uint32(data[14:18])),
	}
	if info.minBlockSize == 0 || info.maxBlockSize == 0 || info.minBlockSize > info.maxBlockSize {
		return streamInfo{}, errors.New("invalid FLAC block size in STREAMINFO")
	}
	if info.sampleRate <= 0 {
		return streamInfo{}, errors.New("invalid FLAC sample rate in STREAMINFO")
	}
	if info.channels <= 0 || info.channels > maxFLACChannels {
		return streamInfo{}, fmt.Errorf("invalid FLAC channel count: %d", info.channels)
	}
	if info.bitsPerSample <= 0 || info.bitsPerSample > 32 {
		return streamInfo{}, fmt.Errorf("unsupported FLAC bit depth: %d", info.bitsPerSample)
	}
	return info, nil
}
