package internal

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/engine"
)

type DecoderConfig struct {
	// StreamInfo is the 34-byte FLAC STREAMINFO metadata block. Demuxers should
	// provide this for demuxed frame packets so the decoder does not parse the
	// native FLAC container itself.
	StreamInfo []byte

	SampleRate    int
	Channels      int
	BitsPerSample int
}

func (DecoderConfig) NodeConfiguration() {}

func DefaultDecoderConfig() DecoderConfig { return DecoderConfig{} }

func NewDecoderConfigFromStreamInfo(stream media.StreamInfo) DecoderConfig {
	config := DefaultDecoderConfig()
	if raw, ok := stream.Metadata.GetRaw(streaminfo.MetadataKey); ok && len(raw) > 0 {
		config.StreamInfo = append([]byte(nil), raw[0]...)
	}
	if stream.Audio.SampleRate > 0 {
		config.SampleRate = stream.Audio.SampleRate
	}
	if channels := stream.Audio.ChannelCount(); channels > 0 {
		config.Channels = channels
	}
	if bitsPerSample := bitDepthFromSampleFormat(stream.Audio.Format); bitsPerSample > 0 {
		config.BitsPerSample = bitsPerSample
	}
	return config
}

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

func NewDecoder(config DecoderConfig) *Decoder {
	decoder := &Decoder{config: config}
	if len(config.StreamInfo) > 0 {
		info, err := streaminfo.Parse(config.StreamInfo)
		if err != nil {
			decoder.configErr = err
		} else {
			decoder.info = info
			decoder.parsed = true
			decoder.initMD5()
		}
	} else if config.SampleRate > 0 || config.Channels > 0 || config.BitsPerSample > 0 {
		decoder.info = streamInfoFromConfig(config)
		if err := streaminfo.Validate(decoder.info); err != nil {
			decoder.configErr = err
		} else {
			decoder.parsed = true
			decoder.initMD5()
		}
	}
	return decoder
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

type hashState struct {
	hash   hash.Hash
	active bool
}

func (d *Decoder) initMD5() {
	if d.info.MD5 != [16]byte{} {
		d.md5Hash.hash = md5.New()
		d.md5Hash.active = true
	}
}

func (d *Decoder) validateFrame(header frameHeader) error {
	if d.frameCount == 0 {
		if header.number != 0 {
			return fmt.Errorf("invalid first FLAC frame number: %d", header.number)
		}
	} else if header.blockingStrategy {
		if header.number != d.sampleCount {
			return fmt.Errorf("unexpected FLAC sample number: got %d, want %d", header.number, d.sampleCount)
		}
	} else if header.number != d.frameCount && header.number != d.sampleCount {
		// Streams written before the blocking-strategy bit was introduced
		// may use sample numbers even though this bit is zero.
		return fmt.Errorf("unexpected FLAC frame/sample number: got %d, want frame %d or sample %d", header.number, d.frameCount, d.sampleCount)
	}
	if d.info.MaxBlockSize > 0 && header.blockSize > int(d.info.MaxBlockSize) {
		return fmt.Errorf("FLAC frame block size %d exceeds STREAMINFO maximum %d", header.blockSize, d.info.MaxBlockSize)
	}
	d.frameCount++
	d.sampleCount += uint64(header.blockSize)
	return nil
}

func (d *Decoder) validateEnd() error {
	if d.info.TotalSamples > 0 && d.sampleCount != d.info.TotalSamples {
		return fmt.Errorf("FLAC sample count mismatch: got %d, want %d", d.sampleCount, d.info.TotalSamples)
	}
	if !d.md5Hash.active {
		return nil
	}
	var got [16]byte
	copy(got[:], d.md5Hash.hash.Sum(nil))
	if got != d.info.MD5 {
		return fmt.Errorf("FLAC PCM MD5 mismatch: got %x, want %x", got, d.info.MD5)
	}
	return nil
}

func (d *Decoder) updateMD5(decoded decodedFrame) {
	if !d.md5Hash.active {
		return
	}
	width := (decoded.header.bitsPerSample + 7) / 8
	var sample [4]byte
	for i := 0; i < decoded.header.blockSize; i++ {
		for ch := 0; ch < decoded.header.channels; ch++ {
			value := decoded.samples[ch][i]
			binary.LittleEndian.PutUint32(sample[:], uint32(value))
			d.md5Hash.hash.Write(sample[:width])
		}
	}
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

func streamInfoFromConfig(config DecoderConfig) streamInfo {
	info := streamInfo{
		MinBlockSize:  1,
		MaxBlockSize:  65535,
		SampleRate:    config.SampleRate,
		Channels:      config.Channels,
		BitsPerSample: config.BitsPerSample,
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
