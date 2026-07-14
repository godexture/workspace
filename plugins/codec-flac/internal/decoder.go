package internal

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/engine"
)

type Decoder struct {
	config DecoderConfig

	buffer      []byte
	parsed      bool
	info        streamInfo
	configErr   error
	flushed     bool
	terminalErr error
	frameCount  uint64
	sampleCount uint64
	md5Hash     hashState
	md5Scratch  []byte
}

type streamInfo = streaminfo.StreamInfo

type frameHeader struct {
	blockSize         int
	sampleRate        int
	channels          int
	channelAssignment uint8
	bitsPerSample     int
	blockingStrategy  bool
	number            uint64
	headerBytes       int
	headerCRC         byte
	frameBytes        int
}

type decodedFrame struct {
	header  frameHeader
	samples [][]int64
	bytes   int
}

func NewDecoder(stream media.StreamInfo, config DecoderConfig) *Decoder {
	decoder := &Decoder{config: config}

	hasRawStreamInfo := false
	if raw, ok := stream.Metadata.GetRaw(streaminfo.MetadataKey); ok && len(raw) > 0 {
		info, err := streaminfo.Parse(raw[0])
		if err != nil {
			decoder.configErr = err
		} else {
			decoder.info = info
			decoder.parsed = true
			decoder.initMD5()
		}
		hasRawStreamInfo = true
	}

	if !hasRawStreamInfo && (stream.Audio.SampleRate > 0 || stream.Audio.ChannelCount() > 0 || stream.Audio.Format != media.SampleFormatUnknown) {
		decoder.info = buildStreamInfo(stream.Audio.SampleRate, stream.Audio.ChannelCount(), bitDepthFromSampleFormat(stream.Audio.Format))
		if err := streaminfo.Validate(decoder.info); err != nil {
			decoder.configErr = err
		} else {
			decoder.parsed = true
			decoder.initMD5()
		}
	}
	return decoder
}

func buildStreamInfo(sampleRate, channels, bitsPerSample int) streamInfo {
	info := streamInfo{
		MinBlockSize:  16,
		MaxBlockSize:  65535,
		SampleRate:    sampleRate,
		Channels:      channels,
		BitsPerSample: bitsPerSample,
	}
	if info.SampleRate <= 0 {
		info.SampleRate = 44100
	}
	if info.Channels <= 0 {
		info.Channels = 2
	}
	if info.BitsPerSample <= 0 {
		info.BitsPerSample = 16
	}
	return info
}

func bitDepthFromSampleFormat(format media.SampleFormat) int {
	switch format.Packed() {
	case media.SampleFormatU8:
		return 8
	case media.SampleFormatS16:
		return 16
	case media.SampleFormatS32:
		return 32
	default:
		return 0
	}
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
	if d.configErr != nil {
		return nil, d.configErr
	}
	if len(d.buffer) == 0 {
		if d.flushed {
			if d.terminalErr != nil {
				return nil, d.terminalErr
			}
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
		d.initMD5()
	}

	if len(d.buffer) == 0 {
		if d.flushed {
			d.terminalErr = d.validateEnd()
			if d.terminalErr != nil {
				return nil, d.terminalErr
			}
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	decoded, err := decodeFrame(d.buffer, d.info)
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
	if err := d.validateFrame(decoded.header); err != nil {
		return nil, err
	}
	d.updateMD5(decoded)

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
	if string(d.buffer[:4]) != streaminfo.Marker {
		return 0, errors.New("not a native FLAC stream")
	}

	offset := 4
	seenStreamInfo := false
	for {
		if len(d.buffer)-offset < 4 {
			return 0, io.ErrUnexpectedEOF
		}
		var header [4]byte
		copy(header[:], d.buffer[offset:offset+4])
		isLast, blockType, length := streaminfo.ParseBlockHeader(header)
		if blockType > 6 {
			return 0, fmt.Errorf("reserved FLAC metadata block type: %d", blockType)
		}
		if length < 0 || length > (1<<24)-1 {
			return 0, errors.New("invalid FLAC metadata length")
		}
		offset += 4
		if len(d.buffer)-offset < length {
			return 0, io.ErrUnexpectedEOF
		}

		if blockType == streaminfo.MetadataTypeStreamInfo {
			if seenStreamInfo {
				return 0, errors.New("duplicate FLAC STREAMINFO block")
			}
			if length != streaminfo.Length {
				return 0, fmt.Errorf("invalid FLAC STREAMINFO length: %d", length)
			}
			info, err := streaminfo.Parse(d.buffer[offset : offset+length])
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
