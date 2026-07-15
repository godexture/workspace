package flac

import (
	"fmt"

	"github.com/godexture/core/domain/media"
)

type DecoderConfig struct{}

func (DecoderConfig) NodeConfiguration() {}

var DefaultDecoderConfig = DecoderConfig{}

const (
	DefaultEncoderBlockSize     = 4096
	DefaultEncoderMaxFixedOrder = 4
	DefaultEncoderMaxLPCOrder   = 32
	DefaultEncoderMaxRiceOrder  = 8
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

	BlockSize                 int
	MaxFixedOrder             int
	MaxLPCOrder               int
	MaxRicePartitionOrder     int
	EnableWastedBits          bool
	EnableStereoDecorrelation bool
	BlockingStrategy          BlockingStrategy
	StreamableSubset          bool
}

var DefaultEncoderConfig = EncoderConfig{
	BlockSize:                 DefaultEncoderBlockSize,
	MaxFixedOrder:             DefaultEncoderMaxFixedOrder,
	MaxLPCOrder:               DefaultEncoderMaxLPCOrder,
	MaxRicePartitionOrder:     DefaultEncoderMaxRiceOrder,
	EnableWastedBits:          true,
	EnableStereoDecorrelation: true,
	StreamableSubset:          true,
}

func (c EncoderConfig) Validate() error {
	if c.BlockSize < 1 || c.BlockSize > 65535 {
		return fmt.Errorf("FLAC encoder block size must be between 1 and 65535: %d", c.BlockSize)
	}
	if c.MaxFixedOrder < 0 || c.MaxFixedOrder > DefaultEncoderMaxFixedOrder {
		return fmt.Errorf("FLAC encoder fixed predictor order must be between 0 and %d: %d", DefaultEncoderMaxFixedOrder, c.MaxFixedOrder)
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

func MergeEncoderConfigForFactory(cfg EncoderConfig, stream media.StreamInfo) EncoderConfig {
	if cfg.SampleRate == 0 && stream.Audio.SampleRate > 0 {
		cfg.SampleRate = stream.Audio.SampleRate
	}
	if cfg.Channels == 0 {
		cfg.Channels = stream.Audio.ChannelCount()
	}
	if cfg.BitsPerSample == 0 {
		cfg.BitsPerSample = stream.Audio.BitsPerSample
		if cfg.BitsPerSample == 0 {
			cfg.BitsPerSample = BitDepthFromSampleFormat(stream.Audio.Format)
		}
	}

	if cfg.MaxFixedOrder > DefaultEncoderMaxFixedOrder {
		cfg.MaxFixedOrder = DefaultEncoderMaxFixedOrder
	}
	if cfg.MaxLPCOrder > 32 {
		cfg.MaxLPCOrder = 32
	}
	if cfg.MaxRicePartitionOrder > 15 {
		cfg.MaxRicePartitionOrder = 15
	}

	return cfg
}

func BitDepthFromSampleFormat(format media.SampleFormat) int {
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
