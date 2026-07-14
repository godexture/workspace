package internal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

func decodeFLACFrame(data []byte, info streamInfo) (decodedFrame, error) {
	reader := bits.New(data)
	header, err := readFrameHeader(reader, info)
	if err != nil {
		return decodedFrame{}, err
	}
	if header.headerBytes < 1 || header.headerBytes > len(data) || hash.CRC8(data[:header.headerBytes-1]) != header.headerCRC {
		return decodedFrame{}, errors.New("invalid FLAC frame header CRC-8")
	}

	samples := make([][]int64, header.channels)
	for ch := 0; ch < header.channels; ch++ {
		bitsPerSample := header.bitsPerSample
		switch header.channelAssignment {
		case 8:
			if ch == 1 {
				bitsPerSample++
			}
		case 9:
			if ch == 0 {
				bitsPerSample++
			}
		case 10:
			if ch == 1 {
				bitsPerSample++
			}
		}

		channelSamples, err := readSubframe(reader, header.blockSize, bitsPerSample)
		if err != nil {
			return decodedFrame{}, fmt.Errorf("decode FLAC subframe %d: %w", ch, err)
		}
		samples[ch] = channelSamples
	}

	decorrelate(samples, header.channelAssignment)
	for ch := range samples {
		if err := validateSampleRange(samples[ch], header.bitsPerSample); err != nil {
			return decodedFrame{}, fmt.Errorf("decoded FLAC channel %d is out of range: %w", ch, err)
		}
	}

	if rem := reader.Position() % 8; rem != 0 {
		padding, err := reader.ReadBits64(uint8(8 - rem))
		if err != nil {
			return decodedFrame{}, err
		}
		if padding != 0 {
			return decodedFrame{}, errors.New("invalid non-zero FLAC frame padding")
		}
	}
	footerStart := reader.BytePos()
	footer, err := reader.ReadBits64(16)
	if err != nil {
		return decodedFrame{}, err
	}
	if footerStart > len(data) || hash.CRC16(data[:footerStart]) != uint16(footer) {
		return decodedFrame{}, errors.New("invalid FLAC frame footer CRC-16")
	}

	if reader.Overrun() {
		return decodedFrame{}, io.ErrUnexpectedEOF
	}

	header.frameBytes = reader.BytePos()
	return decodedFrame{
		header:  header,
		samples: samples,
		bytes:   reader.BytePos(),
	}, nil
}

func buildAudioFrame(decoded decodedFrame) (*media.AudioFrame, error) {
	format := streaminfo.SampleFormat(decoded.header.bitsPerSample)
	layout := streaminfo.ChannelLayout(decoded.header.channels)
	frame := media.NewAudioFrame(
		format,
		layout,
		decoded.header.sampleRate,
		decoded.header.blockSize,
		media.WithAudioBitsPerSample(decoded.header.bitsPerSample),
	)
	plane := frame.Planes()[0]

	bytesPerSample := format.BytesPerSample()
	for sampleIndex := 0; sampleIndex < decoded.header.blockSize; sampleIndex++ {
		for channel := 0; channel < decoded.header.channels; channel++ {
			offset := (sampleIndex*decoded.header.channels + channel) * bytesPerSample
			value := decoded.samples[channel][sampleIndex]
			switch format {
			case media.SampleFormatS16:
				binary.LittleEndian.PutUint16(plane[offset:offset+2], uint16(int16(value)))
			case media.SampleFormatS32:
				binary.LittleEndian.PutUint32(plane[offset:offset+4], uint32(value))
			default:
				return nil, fmt.Errorf("unsupported FLAC output format: %s", format)
			}
		}
	}
	return frame, nil
}
