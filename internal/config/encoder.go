package config

import (
	"fmt"
	"log"

	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/core/domain/media"
)

const (
	DefaultEncoderBlockSize     = 4096
	DefaultEncoderMaxFixedOrder = 4
	DefaultEncoderMaxLPCOrder   = 32
	DefaultEncoderMaxRiceOrder  = 8
	DefaultLPCPrecision         = 15
)

type BlockSplitMode uint8

const (
	BlockSplitEstimated BlockSplitMode = iota
	BlockSplitExact
)

type StereoMode uint8

const (
	StereoIndependent StereoMode = iota
	StereoAdaptive
	StereoExhaustive
)

// OrderSearchMode controls how predictor orders are selected. Estimated mode
// only narrows candidates; emitted frames and their final costs stay exact.
type OrderSearchMode uint8

const (
	OrderSearchEstimated OrderSearchMode = iota
	OrderSearchExhaustive
)

// RiceCostMode controls Rice parameter selection. Estimated mode only selects
// parameters approximately; the selected coding is always costed exactly.
type RiceCostMode uint8

const (
	RiceCostEstimated RiceCostMode = iota
	RiceCostExact
)

type EncoderConfig struct {
	// Stream parameters.
	SampleRate    int
	Channels      int
	BitsPerSample int

	// Compression parameters.
	BlockSize             int
	MaxFixedOrder         int
	MaxLPCOrder           int
	MaxRicePartitionOrder int
	LPCPrecision          int
	EnablePrecisionSearch bool
	EnableWastedBits      bool
	StereoMode            StereoMode
	FixedOrderSearch      OrderSearchMode
	LPCOrderSearch        OrderSearchMode
	RiceCost              RiceCostMode
	Apodizations          []flac.Apodization
	BlockSplitDepth       int
	BlockSplitMode        BlockSplitMode
	StreamableSubset      bool
}

var DefaultEncoderConfig = GetPreset(5)

func GetPreset(level int) EncoderConfig {
	if level < 0 {
		log.Printf("WARNING: FLAC compression level %d is outside 0..8; using 0", level)
		level = 0
	} else if level > 8 {
		log.Printf("WARNING: FLAC compression level %d is outside 0..8; using 8", level)
		level = 8
	}

	blockSize, maxLPC, maxRice := 4096, 8, 4
	mode := StereoExhaustive
	apodizations := []flac.Apodization{flac.Tukey(0.5)}
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
		maxRice, apodizations = 6, flac.SubdivideTukey(2, 0.5)
	case 7:
		maxLPC, maxRice, apodizations = 12, 6, flac.SubdivideTukey(2, 0.5)
	case 8:
		maxLPC, maxRice, apodizations = 12, 6, flac.SubdivideTukey(3, 0.5)
	}

	config := EncoderConfig{
		BlockSize: blockSize, MaxFixedOrder: DefaultEncoderMaxFixedOrder, MaxLPCOrder: maxLPC,
		MaxRicePartitionOrder: maxRice, LPCPrecision: DefaultLPCPrecision,
		EnablePrecisionSearch: false, EnableWastedBits: true, StereoMode: mode,
		FixedOrderSearch: OrderSearchEstimated, LPCOrderSearch: OrderSearchEstimated, RiceCost: RiceCostEstimated, Apodizations: apodizations,
		StreamableSubset: true,
	}

	switch level {
	case 7:
		config.BlockSplitDepth, config.BlockSplitMode = 2, BlockSplitEstimated
	case 8:
		config.BlockSplitDepth, config.BlockSplitMode = 2, BlockSplitExact
	}

	return config
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
	if c.FixedOrderSearch > OrderSearchExhaustive || c.LPCOrderSearch > OrderSearchExhaustive {
		return fmt.Errorf("invalid FLAC encoder order search mode")
	}
	if c.RiceCost > RiceCostExact {
		return fmt.Errorf("invalid FLAC encoder Rice cost mode")
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
	if c.BlockSplitDepth < 0 {
		return fmt.Errorf("FLAC block split depth must be non-negative: %d", c.BlockSplitDepth)
	}
	if c.BlockSplitMode > BlockSplitExact {
		return fmt.Errorf("invalid FLAC block split mode: %d", c.BlockSplitMode)
	}
	if c.BlockSplitDepth > 0 {
		if c.BlockSplitDepth > 15 {
			return fmt.Errorf("FLAC block split depth exceeds supported range: %d", c.BlockSplitDepth)
		}
		parts := 1 << c.BlockSplitDepth
		if c.BlockSize%parts != 0 {
			return fmt.Errorf("FLAC block size %d must be divisible by %d at split depth %d", c.BlockSize, parts, c.BlockSplitDepth)
		}
		if c.BlockSize>>c.BlockSplitDepth < 16 {
			return fmt.Errorf("FLAC block split depth %d makes blocks smaller than 16 samples", c.BlockSplitDepth)
		}
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
