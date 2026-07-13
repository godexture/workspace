package internal

import (
	"fmt"

	"github.com/godexture/core/domain/media"
)

const (
	defaultEncoderBlockSize     = 4096
	defaultEncoderMaxFixedOrder = 4
)

type EncoderConfig struct {
	SampleRate    int
	Channels      int
	BitsPerSample int

	BlockSize     int
	MaxFixedOrder int
}

func (EncoderConfig) NodeConfiguration() {}

func DefaultEncoderConfig() EncoderConfig {
	return EncoderConfig{
		BlockSize:     defaultEncoderBlockSize,
		MaxFixedOrder: defaultEncoderMaxFixedOrder,
	}
}

func (c *EncoderConfig) applyDefaults() {
	if c.BlockSize <= 0 {
		c.BlockSize = defaultEncoderBlockSize
	}
	if c.MaxFixedOrder <= 0 {
		c.MaxFixedOrder = defaultEncoderMaxFixedOrder
	}
	if c.MaxFixedOrder > defaultEncoderMaxFixedOrder {
		c.MaxFixedOrder = defaultEncoderMaxFixedOrder
	}
}

func (c EncoderConfig) validate() error {
	if c.BlockSize < 1 || c.BlockSize > 16384 {
		return fmt.Errorf("FLAC encoder block size must be between 1 and 16384: %d", c.BlockSize)
	}
	if c.MaxFixedOrder < 0 || c.MaxFixedOrder > defaultEncoderMaxFixedOrder {
		return fmt.Errorf("FLAC encoder fixed predictor order must be between 0 and %d: %d", defaultEncoderMaxFixedOrder, c.MaxFixedOrder)
	}
	if c.SampleRate < 0 {
		return fmt.Errorf("invalid FLAC encoder sample rate: %d", c.SampleRate)
	}
	if c.Channels < 0 || c.Channels > 8 {
		return fmt.Errorf("invalid FLAC encoder channel count: %d", c.Channels)
	}
	if c.BitsPerSample != 0 && c.BitsPerSample != 16 && c.BitsPerSample != 24 && c.BitsPerSample != 32 {
		return fmt.Errorf("unsupported FLAC encoder bit depth: %d", c.BitsPerSample)
	}
	return nil
}

func MergeEncoderConfigForFactory(config EncoderConfig, stream media.StreamInfo) EncoderConfig {
	config.applyDefaults()
	if config.SampleRate <= 0 && stream.Audio.SampleRate > 0 {
		config.SampleRate = stream.Audio.SampleRate
	}
	if config.Channels <= 0 {
		config.Channels = stream.Audio.ChannelCount()
	}
	if config.BitsPerSample <= 0 {
		config.BitsPerSample = bitDepthFromSampleFormat(stream.Audio.Format)
	}
	return config
}
