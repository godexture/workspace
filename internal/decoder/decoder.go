package decoder

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/engine"
)

type hashState struct {
	hash   hash.Hash
	active bool
}

type Decoder struct {
	config flac.DecoderConfig

	buffer       []byte
	bufferOffset int
	workspace    decodeWorkspace
	parsed       bool
	info         streaminfo.StreamInfo
	configErr    error
	flushed      bool
	terminalErr  error
	frameCount   uint64
	sampleCount  uint64
	md5Hash      hashState
	md5Scratch   []byte
}

func NewDecoder(stream media.StreamInfo, config flac.DecoderConfig) *Decoder {
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
		bitsPerSample := stream.Audio.BitsPerSample
		if bitsPerSample == 0 {
			bitsPerSample = flac.BitDepthFromSampleFormat(stream.Audio.Format)
		}
		decoder.info = buildStreamInfo(stream.Audio.SampleRate, stream.Audio.ChannelCount(), bitsPerSample)
		if err := streaminfo.Validate(decoder.info); err != nil {
			decoder.configErr = err
		} else {
			decoder.parsed = true
			decoder.initMD5()
		}
	}
	return decoder
}

func buildStreamInfo(sampleRate, channels, bitsPerSample int) streaminfo.StreamInfo {
	info := streaminfo.StreamInfo{
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

func (d *Decoder) SendPacket(pkt *media.Packet) error {
	if pkt == nil {
		return errors.New("flac decoder requires a non-nil packet")
	}
	if d.flushed {
		return engine.ErrEOF
	}
	d.appendInput(pkt.Data())
	return nil
}

func (d *Decoder) ReceiveFrame() (*media.Frame, error) {
	if d.configErr != nil {
		return nil, d.configErr
	}
	if len(d.input()) == 0 {
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
		d.consumeInput(consumed)
		d.parsed = true
		d.initMD5()
	}

	if len(d.input()) == 0 {
		if d.flushed {
			d.terminalErr = d.validateEnd()
			if d.terminalErr != nil {
				return nil, d.terminalErr
			}
			return nil, engine.ErrEOF
		}
		return nil, engine.ErrEAGAIN
	}

	decoded, err := decodeFrame(d.input(), d.info, &d.workspace)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			if d.flushed {
				return nil, err
			}
			return nil, engine.ErrEAGAIN
		}
		return nil, err
	}
	d.consumeInput(decoded.Bytes)
	if err := d.validateFrame(decoded.Header); err != nil {
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
	data := d.input()
	if len(data) < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	if string(data[:4]) != streaminfo.Marker {
		return 0, errors.New("not a native FLAC stream")
	}

	offset := 4
	seenStreamInfo := false
	for {
		if len(data)-offset < 4 {
			return 0, io.ErrUnexpectedEOF
		}
		var header [4]byte
		copy(header[:], data[offset:offset+4])
		isLast, blockType, length := streaminfo.ParseBlockHeader(header)
		if blockType > 6 {
			return 0, fmt.Errorf("reserved FLAC metadata block type: %d", blockType)
		}
		if length < 0 || length > (1<<24)-1 {
			return 0, errors.New("invalid FLAC metadata length")
		}
		offset += 4
		if len(data)-offset < length {
			return 0, io.ErrUnexpectedEOF
		}

		if blockType == streaminfo.MetadataTypeStreamInfo {
			if seenStreamInfo {
				return 0, errors.New("duplicate FLAC STREAMINFO block")
			}
			if length != streaminfo.Length {
				return 0, fmt.Errorf("invalid FLAC STREAMINFO length: %d", length)
			}
			info, err := streaminfo.Parse(data[offset : offset+length])
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

func (d *Decoder) input() []byte {
	return d.buffer[d.bufferOffset:]
}

func (d *Decoder) appendInput(data []byte) {
	if len(data) == 0 {
		return
	}
	if d.bufferOffset == len(d.buffer) {
		d.buffer = d.buffer[:0]
		d.bufferOffset = 0
	}
	if cap(d.buffer)-len(d.buffer) < len(data) && d.bufferOffset > 0 {
		unread := copy(d.buffer, d.buffer[d.bufferOffset:])
		d.buffer = d.buffer[:unread]
		d.bufferOffset = 0
	}
	d.buffer = append(d.buffer, data...)
}

func (d *Decoder) consumeInput(n int) {
	d.bufferOffset += n
	if d.bufferOffset == len(d.buffer) {
		d.buffer = d.buffer[:0]
		d.bufferOffset = 0
	}
}

func (d *Decoder) initMD5() {
	if d.info.MD5 != [16]byte{} {
		d.md5Hash.hash = md5.New()
		d.md5Hash.active = true
	}
}

func (d *Decoder) validateFrame(header flac.FrameHeader) error {
	if d.frameCount == 0 {
		if header.Number != 0 {
			return fmt.Errorf("invalid first FLAC frame number: %d", header.Number)
		}
	} else if header.BlockingStrategy {
		if header.Number != d.sampleCount {
			return fmt.Errorf("unexpected FLAC sample number: got %d, want %d", header.Number, d.sampleCount)
		}
	} else if header.Number != d.frameCount && header.Number != d.sampleCount {
		return fmt.Errorf("unexpected FLAC frame/sample number: got %d, want frame %d or sample %d", header.Number, d.frameCount, d.sampleCount)
	}
	if d.info.MaxBlockSize > 0 && header.BlockSize > int(d.info.MaxBlockSize) {
		return fmt.Errorf("FLAC frame block size %d exceeds STREAMINFO maximum %d", header.BlockSize, d.info.MaxBlockSize)
	}
	d.frameCount++
	d.sampleCount += uint64(header.BlockSize)
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

func (d *Decoder) updateMD5(decoded *flac.Frame) {
	if !d.md5Hash.active {
		return
	}
	width := (decoded.Header.BitsPerSample + 7) / 8
	needed := decoded.Header.BlockSize * decoded.Header.Channels * width
	if cap(d.md5Scratch) < needed+4 {
		d.md5Scratch = make([]byte, needed+4)
	}
	buf := d.md5Scratch[:needed+4]
	offset := 0
	for i := 0; i < decoded.Header.BlockSize; i++ {
		for ch := 0; ch < decoded.Header.Channels; ch++ {
			binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(decoded.Samples[ch][i]))
			offset += width
		}
	}
	d.md5Hash.hash.Write(buf[:needed])
}
