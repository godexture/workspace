package internal

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-flac/streaminfo"
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
