package decoder

import (
	"errors"
	"fmt"
	"io"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/frame"
	"github.com/godexture/format-flac/streaminfo"
	"github.com/godexture/sdk/bits"
	"github.com/godexture/sdk/hash"
)

type decodeWorkspace struct {
	reader  bits.Reader
	samples [][]int64
}

func DecodeFrame(data []byte, info streaminfo.StreamInfo) (*flac.Frame, error) {
	var workspace decodeWorkspace
	return decodeFrame(data, info, true, &workspace)
}

func decodeFrame(data []byte, info streaminfo.StreamInfo, strict bool, workspace *decodeWorkspace) (*flac.Frame, error) {
	reader := &workspace.reader
	reader.Init(data, 0, int32(len(data))*8)
	header, err := frame.ParseHeader(data, info)
	if err != nil {
		return nil, err
	}
	reader.Seek(int32(header.HeaderBytes * 8))

	samples := workspace.sampleBuffers(header.Channels, header.BlockSize)
	for ch := 0; ch < header.Channels; ch++ {
		bitsPerSample := header.BitsPerSample
		switch header.ChannelAssignment {
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

		if err := decodeSubframe(reader, samples[ch], bitsPerSample, strict); err != nil {
			return nil, fmt.Errorf("decode FLAC subframe %d: %w", ch, err)
		}
	}

	Decorrelate(samples, header.ChannelAssignment)
	if strict {
		for ch := range samples {
			if err := flac.ValidateSampleRange(samples[ch], header.BitsPerSample); err != nil {
				return nil, fmt.Errorf("decoded FLAC channel %d is out of range: %w", ch, err)
			}
		}
	}

	if rem := reader.Position() % 8; rem != 0 {
		padding, err := reader.ReadBits32(uint8(8 - rem))
		if err != nil {
			return nil, err
		}
		if strict && padding != 0 {
			return nil, errors.New("invalid non-zero FLAC frame padding")
		}
	}
	footerStart := reader.BytePos()
	footer, err := reader.ReadBits32(16)
	if err != nil {
		return nil, err
	}
	if strict && (footerStart > len(data) || hash.CRC16(data[:footerStart]) != uint16(footer)) {
		return nil, errors.New("invalid FLAC frame footer CRC-16")
	}

	if reader.Overrun() {
		return nil, io.ErrUnexpectedEOF
	}

	header.FrameBytes = reader.BytePos()
	return &flac.Frame{
		Header:  header,
		Samples: samples,
		Bytes:   reader.BytePos(),
	}, nil
}

func (w *decodeWorkspace) sampleBuffers(channels, blockSize int) [][]int64 {
	if cap(w.samples) < channels {
		w.samples = make([][]int64, channels)
	} else {
		w.samples = w.samples[:channels]
	}
	for ch := range w.samples {
		if cap(w.samples[ch]) < blockSize {
			w.samples[ch] = make([]int64, blockSize)
		} else {
			w.samples[ch] = w.samples[ch][:blockSize]
		}
	}
	return w.samples
}

func buildAudioFrame(decoded *flac.Frame, pts media.Pts) (*media.AudioFrame, error) {
	format := streaminfo.SampleFormat(decoded.Header.BitsPerSample)
	layout := streaminfo.ChannelLayout(decoded.Header.Channels)
	frame := media.NewAudioFrame(
		format,
		layout,
		decoded.Header.SampleRate,
		decoded.Header.BlockSize,
		media.WithAudioBitsPerSample(decoded.Header.BitsPerSample),
		media.WithAudioPts(pts),
	)
	plane := frame.Planes()[0]

	switch format {
	case media.SampleFormatS16:
		interleaveS16(plane, decoded.Samples, decoded.Header.BlockSize, decoded.Header.Channels)
	case media.SampleFormatS24:
		interleaveS24(plane, decoded.Samples, decoded.Header.BlockSize, decoded.Header.Channels)
	case media.SampleFormatS32:
		interleaveS32(plane, decoded.Samples, decoded.Header.BlockSize, decoded.Header.Channels)
	default:
		return nil, fmt.Errorf("unsupported FLAC output format: %s", format)
	}
	return frame, nil
}
