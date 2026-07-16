package flac

import (
	"fmt"

	"github.com/godexture/core/domain/media"
)

type DecoderConfig struct{}

var DefaultDecoderConfig = DecoderConfig{}

const (
	DefaultEncoderBlockSize     = 4096
	DefaultEncoderMaxFixedOrder = 4
	DefaultEncoderMaxLPCOrder   = 32
	DefaultEncoderMaxRiceOrder  = 8
	DefaultLPCPrecision         = 15
)

type BlockingStrategy uint8

const (
	FixedBlocking BlockingStrategy = iota
	VariableBlocking
)

type StereoMode uint8

const (
	StereoIndependent StereoMode = iota
	StereoAdaptive
	StereoExhaustive
)

type EncoderConfig struct {
	SampleRate    int
	Channels      int
	BitsPerSample int

	BlockSize              int
	MaxFixedOrder          int
	MaxLPCOrder            int
	MaxRicePartitionOrder  int
	LPCPrecision           int
	EnablePrecisionSearch  bool
	EnableWastedBits       bool
	StereoMode             StereoMode
	EnableExhaustiveSearch bool
	Apodizations           []Apodization
	BlockingStrategy       BlockingStrategy
	StreamableSubset       bool
}

var DefaultEncoderConfig = GetPreset(5)

func GetPreset(level int) EncoderConfig {
	blockSize, maxLPC, maxRice := 4096, 8, 4
	mode := StereoExhaustive
	apodizations := []Apodization{Tukey(0.5)}
	switch level {
	case 0, 1, 2:
		blockSize, maxLPC, maxRice = 1152, 0, 3
		mode = StereoMode(level)
	case 3:
		maxLPC, mode = 6, StereoIndependent
	case 4:
		mode = StereoAdaptive
	case 5:
		maxRice = 5
	case 6:
		maxRice, apodizations = 6, SubdivideTukey(2, 0.5)
	case 7:
		maxLPC, maxRice, apodizations = 12, 6, SubdivideTukey(2, 0.5)
	case 8:
		maxLPC, maxRice, apodizations = 12, 6, SubdivideTukey(3, 0.5)
	}
	return EncoderConfig{
		BlockSize: blockSize, MaxFixedOrder: DefaultEncoderMaxFixedOrder, MaxLPCOrder: maxLPC,
		MaxRicePartitionOrder: maxRice, LPCPrecision: DefaultLPCPrecision,
		EnablePrecisionSearch: false, EnableWastedBits: true, StereoMode: mode,
		EnableExhaustiveSearch: false, Apodizations: apodizations,
		BlockingStrategy: FixedBlocking, StreamableSubset: true,
	}
}

func (c EncoderConfig) Validate() error {
	if c.BlockSize < 16 || c.BlockSize > 65535 {
		return fmt.Errorf("FLAC encoder block size must be between 16 and 65535: %d", c.BlockSize)
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
	if c.LPCPrecision != 0 && (c.LPCPrecision < 4 || c.LPCPrecision > 15) {
		return fmt.Errorf("FLAC LPC precision must be between 4 and 15: %d", c.LPCPrecision)
	}
	if c.StereoMode > StereoExhaustive {
		return fmt.Errorf("invalid FLAC stereo mode: %d", c.StereoMode)
	}
	if len(c.Apodizations) > 32 {
		return fmt.Errorf("FLAC encoder supports at most 32 apodization windows: %d", len(c.Apodizations))
	}
	for i, apodization := range c.Apodizations {
		if apodization == nil {
			return fmt.Errorf("FLAC apodization %d is nil", i)
		}
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
	if cfg.LPCPrecision > 15 {
		cfg.LPCPrecision = 15
	}

	return cfg
}

func BitDepthFromSampleFormat(format media.SampleFormat) int {
	switch format.Packed() {
	case media.SampleFormatU8:
		return 8
	case media.SampleFormatS16:
		return 16
	case media.SampleFormatS24:
		return 24
	case media.SampleFormatS32:
		return 32
	default:
		return 0
	}
}
