package internal

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
)

func (m *Muxer) recordFrame(blockSize, frameSize int) {
	blockSizeValue := streamInfoBlockSize(blockSize)
	frameSizeValue := uint32(frameSize)
	if m.frameCount == 0 {
		m.minBlockSize = blockSizeValue
		m.maxBlockSize = blockSizeValue
		m.minFrameSize = frameSizeValue
		m.maxFrameSize = frameSizeValue
	} else {
		if m.pendingBlockSize < m.minBlockSize {
			m.minBlockSize = m.pendingBlockSize
		}
		if blockSizeValue > m.maxBlockSize {
			m.maxBlockSize = blockSizeValue
		}
		if frameSizeValue < m.minFrameSize {
			m.minFrameSize = frameSizeValue
		}
		if frameSizeValue > m.maxFrameSize {
			m.maxFrameSize = frameSizeValue
		}
	}
	m.pendingBlockSize = blockSizeValue
	m.totalSamples += uint64(blockSize)
	m.frameCount++
}

func (m *Muxer) finalStreamInfo() streaminfo.StreamInfo {
	info := m.info
	if m.frameCount == 0 {
		return info
	}
	info.MinBlockSize = m.minBlockSize
	info.MaxBlockSize = m.maxBlockSize
	info.MinFrameSize = m.minFrameSize
	info.MaxFrameSize = m.maxFrameSize
	info.TotalSamples = m.totalSamples
	return info
}

func streamInfoBlockSize(blockSize int) uint16 {
	if blockSize < 16 {
		return 16
	}
	if blockSize > 65535 {
		return 65535
	}
	return uint16(blockSize)
}

func buildStreamInfo(stream media.StreamInfo) (streaminfo.StreamInfo, error) {
	attr := stream.MediaAttributes.Audio
	sampleRate := attr.SampleRate
	channels := attr.ChannelCount()
	bitsPerSample := attr.BitsPerSample

	var inherited streaminfo.StreamInfo
	if raw, ok := stream.Metadata.GetRaw(streaminfo.MetadataKey); ok && len(raw) > 0 {
		parsed, err := streaminfo.Parse(raw[0])
		if err != nil {
			return streaminfo.StreamInfo{}, fmt.Errorf("flac muxer invalid STREAMINFO metadata: %w", err)
		}
		inherited = parsed
		if sampleRate <= 0 {
			sampleRate = parsed.SampleRate
		}
		if channels <= 0 {
			channels = parsed.Channels
		}
		if bitsPerSample <= 0 {
			bitsPerSample = parsed.BitsPerSample
		}
	}
	if bitsPerSample == 0 {
		switch attr.Format.Packed() {
		case media.SampleFormatS16, media.SampleFormatS24, media.SampleFormatS32:
			bitsPerSample = attr.Format.Packed().BitsPerSample()
		default:
			return streaminfo.StreamInfo{}, fmt.Errorf("flac muxer unsupported sample format: %s", attr.Format)
		}
	}

	info := streaminfo.StreamInfo{
		MinBlockSize:  16,
		MaxBlockSize:  65535,
		SampleRate:    sampleRate,
		Channels:      channels,
		BitsPerSample: bitsPerSample,
		TotalSamples:  inherited.TotalSamples,
		MD5:           inherited.MD5,
	}
	if err := streaminfo.Validate(info); err != nil {
		return streaminfo.StreamInfo{}, fmt.Errorf("flac muxer invalid stream attributes: %w", err)
	}
	return info, nil
}
