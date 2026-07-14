package internal

import (
	"fmt"

	"github.com/godexture/core/domain/media"
)

const (
	defaultEncoderBlockSize     = 4096
	defaultEncoderMaxFixedOrder = 4
	defaultEncoderMaxLPCOrder   = 32
	defaultEncoderMaxRiceOrder  = 8
)

type BlockingStrategy uint8

const (
	FixedBlocking BlockingStrategy = iota
	VariableBlocking
)

type EncoderConfig struct {
	SampleRate    int
	Channels      int
	BitsPerSample int

	BlockSize             int
	MaxFixedOrder         int
	MaxLPCOrder           int
	MaxRicePartitionOrder int
	EnableWastedBits      bool
	EnableStereoDecorrel  bool
	BlockingStrategy      BlockingStrategy
	StreamableSubset      bool
}

func (EncoderConfig) NodeConfiguration() {}

var DefaultEncoderConfig = EncoderConfig{
	BlockSize:             defaultEncoderBlockSize,
	MaxFixedOrder:         defaultEncoderMaxFixedOrder,
	MaxLPCOrder:           defaultEncoderMaxLPCOrder,
	MaxRicePartitionOrder: defaultEncoderMaxRiceOrder,
	EnableWastedBits:      true,
	EnableStereoDecorrel:  true,
	StreamableSubset:      true,
}

func (c *EncoderConfig) applyDefaults() {
	if c.BlockSize <= 0 {
		c.BlockSize = defaultEncoderBlockSize
	}
	// Zero is a meaningful explicit order (and is therefore not replaced by
	// the default). Callers that want defaults should use
	// DefaultEncoderConfig.
	if c.MaxFixedOrder > defaultEncoderMaxFixedOrder {
		c.MaxFixedOrder = defaultEncoderMaxFixedOrder
	}
	if c.MaxLPCOrder > 32 {
		c.MaxLPCOrder = 32
	}
	if c.MaxRicePartitionOrder > 15 {
		c.MaxRicePartitionOrder = 15
	}
}

func (c EncoderConfig) validate() error {
	if c.BlockSize < 1 || c.BlockSize > 65535 {
		return fmt.Errorf("FLAC encoder block size must be between 1 and 65535: %d", c.BlockSize)
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
	if c.BitsPerSample != 0 && (c.BitsPerSample < 4 || c.BitsPerSample > 32) {
		return fmt.Errorf("unsupported FLAC encoder bit depth: %d", c.BitsPerSample)
	}
	if c.MaxLPCOrder < 0 || c.MaxLPCOrder > 32 {
		return fmt.Errorf("invalid FLAC LPC order: %d", c.MaxLPCOrder)
	}
	if c.MaxRicePartitionOrder < 0 || c.MaxRicePartitionOrder > 15 {
		return fmt.Errorf("invalid FLAC Rice partition order: %d", c.MaxRicePartitionOrder)
	}
	if c.StreamableSubset && c.MaxRicePartitionOrder > 8 {
		return fmt.Errorf("streamable-subset FLAC Rice partition order must be <= 8: %d", c.MaxRicePartitionOrder)
	}
	if c.BlockingStrategy != FixedBlocking && c.BlockingStrategy != VariableBlocking {
		return fmt.Errorf("invalid FLAC blocking strategy: %d", c.BlockingStrategy)
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
