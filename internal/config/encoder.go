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

type BlockSplitMode string

const (
	BlockSplitEstimated BlockSplitMode = "estimated"
	BlockSplitExact     BlockSplitMode = "exact"
)

type StereoMode string

const (
	StereoIndependent StereoMode = "independent"
	StereoAdaptive    StereoMode = "adaptive"
	StereoExhaustive  StereoMode = "exhaustive"
)

// OrderSearchMode controls how predictor orders are selected. Estimated mode
// only narrows candidates; emitted frames and their final costs stay exact.
type OrderSearchMode string

const (
	OrderSearchEstimated  OrderSearchMode = "estimated"
	OrderSearchExhaustive OrderSearchMode = "exhaustive"
)

// RiceCostMode controls Rice parameter selection. Estimated mode only selects
// parameters approximately; the selected coding is always costed exactly.
type RiceCostMode string

const (
	RiceCostEstimated RiceCostMode = "estimated"
	RiceCostExact     RiceCostMode = "exact"
)

type EncoderConfig struct {
	// Stream parameters.
	sampleRate    int
	channels      int
	bitsPerSample int

	// Compression parameters.
	BlockSize             int             `name:"block-size" help:"FLAC block size in samples"`
	MaxFixedOrder         int             `name:"max-fixed-order" help:"Maximum fixed predictor order"`
	MaxLPCOrder           int             `name:"max-lpc-order" help:"Maximum LPC predictor order"`
	MaxRicePartitionOrder int             `name:"max-rice-partition-order" help:"Maximum Rice partition order"`
	LPCPrecision          int             `name:"lpc-precision" help:"LPC coefficient precision"`
	EnablePrecisionSearch bool            `name:"precision-search" help:"Enable LPC precision search"`
	EnableWastedBits      bool            `name:"wasted-bits" help:"Enable wasted-bits optimization"`
	StereoMode            StereoMode      `name:"stereo-mode" help:"Stereo coding mode"`
	FixedOrderSearch      OrderSearchMode `name:"fixed-order-search" help:"Fixed predictor order search"`
	LPCOrderSearch        OrderSearchMode `name:"lpc-order-search" help:"LPC predictor order search"`
	RiceCost              RiceCostMode    `name:"rice-cost" help:"Rice parameter cost mode"`
	Apodizations          []flac.Apodization
	BlockSplitDepth       int            `name:"block-split-depth" help:"Block split depth"`
	BlockSplitMode        BlockSplitMode `name:"block-split-mode" help:"Block split strategy"`
	StreamableSubset      bool           `name:"streamable-subset" help:"Restrict output to the FLAC streamable subset"`
}

func (c EncoderConfig) SampleRate() int    { return c.sampleRate }
func (c EncoderConfig) Channels() int      { return c.channels }
func (c EncoderConfig) BitsPerSample() int { return c.bitsPerSample }

const (
	MinPresetLevel = 0
	MaxPresetLevel = 23
)

var DefaultEncoderConfig = GetPreset(12)

type presetTier struct {
	stereoMode       StereoMode
	maxLPCOrder      int
	maxRiceOrder     int
	apodizationParts int
	blockSplitDepth  int
	riceCostExact    bool
	precisionSearch  bool
}

var presetTiers = [MaxPresetLevel + 1]presetTier{
	// stereoMode, maxLPCOrder, maxRiceOrder, apodizationParts, blockSplitDepth, riceCostExact, precisionSearch
	{StereoIndependent, 0, 0, 1, 0, false, false},
	{StereoIndependent, 0, 1, 1, 0, false, false},
	{StereoIndependent, 2, 1, 1, 0, false, false},
	{StereoIndependent, 4, 2, 1, 0, false, false},
	{StereoAdaptive, 4, 2, 1, 0, false, false},
	{StereoAdaptive, 6, 2, 1, 0, false, false},
	{StereoAdaptive, 6, 3, 1, 0, false, false},
	{StereoAdaptive, 8, 3, 1, 0, false, false},
	{StereoExhaustive, 8, 3, 1, 0, false, false},
	{StereoExhaustive, 8, 4, 1, 0, false, false},
	{StereoExhaustive, 8, 5, 1, 0, false, false},
	{StereoExhaustive, 8, 5, 1, 0, false, false},
	{StereoExhaustive, 10, 5, 1, 0, false, false},
	{StereoExhaustive, 10, 6, 1, 0, false, false},
	{StereoExhaustive, 10, 6, 1, 0, false, false},
	{StereoExhaustive, 12, 6, 1, 0, false, false},
	{StereoExhaustive, 12, 6, 2, 2, true, false},
	{StereoExhaustive, 12, 7, 2, 2, true, false},
	{StereoExhaustive, 12, 7, 3, 2, true, false},
	{StereoExhaustive, 14, 7, 3, 3, true, false},
	{StereoExhaustive, 14, 8, 3, 3, true, false},
	{StereoExhaustive, 16, 8, 4, 3, true, true},
	{StereoExhaustive, 18, 8, 5, 4, true, true},
	{StereoExhaustive, 20, 8, 6, 4, true, true},
}

func GetPreset(level int) EncoderConfig {
	if level < MinPresetLevel {
		log.Printf("WARNING: FLAC compression level %d is outside %d..%d; using %d", level, MinPresetLevel, MaxPresetLevel, MinPresetLevel)
		level = MinPresetLevel
	} else if level > MaxPresetLevel {
		log.Printf("WARNING: FLAC compression level %d is outside %d..%d; using %d", level, MinPresetLevel, MaxPresetLevel, MaxPresetLevel)
		level = MaxPresetLevel
	}
	tier := presetTiers[level]

	apodizations := []flac.Apodization{flac.Tukey(0.5)}
	if tier.apodizationParts > 1 {
		apodizations = flac.SubdivideTukey(tier.apodizationParts, 0.5)
	}

	riceCost := RiceCostEstimated
	if tier.riceCostExact {
		riceCost = RiceCostExact
	}
	blockSplitMode := BlockSplitEstimated
	if tier.blockSplitDepth > 0 {
		blockSplitMode = BlockSplitExact
	}

	return EncoderConfig{
		BlockSize: DefaultEncoderBlockSize, MaxFixedOrder: DefaultEncoderMaxFixedOrder, MaxLPCOrder: tier.maxLPCOrder,
		MaxRicePartitionOrder: tier.maxRiceOrder, LPCPrecision: DefaultLPCPrecision,
		EnablePrecisionSearch: tier.precisionSearch, EnableWastedBits: true, StereoMode: tier.stereoMode,
		FixedOrderSearch: OrderSearchEstimated, LPCOrderSearch: OrderSearchEstimated, RiceCost: riceCost, Apodizations: apodizations,
		StreamableSubset: true,
		BlockSplitMode:   blockSplitMode, BlockSplitDepth: tier.blockSplitDepth,
	}
}

func (c EncoderConfig) Validate() error {
	if c.BlockSize < 16 || c.BlockSize > 65535 {
		return fmt.Errorf("FLAC encoder block size must be between 16 and 65535: %d", c.BlockSize)
	}
	if c.MaxFixedOrder < 0 || c.MaxFixedOrder > DefaultEncoderMaxFixedOrder {
		return fmt.Errorf("FLAC encoder fixed predictor order must be between 0 and %d: %d", DefaultEncoderMaxFixedOrder, c.MaxFixedOrder)
	}
	if c.sampleRate < 0 {
		return fmt.Errorf("invalid FLAC encoder sample rate: %d", c.sampleRate)
	}
	if c.channels < 0 || c.channels > 8 {
		return fmt.Errorf("invalid FLAC encoder channel count: %d", c.channels)
	}
	if c.bitsPerSample != 0 && (c.bitsPerSample < 4 || c.bitsPerSample > 32) {
		return fmt.Errorf("unsupported FLAC encoder bit depth: %d", c.bitsPerSample)
	}
	if c.MaxLPCOrder < 0 || c.MaxLPCOrder > 32 {
		return fmt.Errorf("invalid FLAC LPC order: %d", c.MaxLPCOrder)
	}
	if c.LPCPrecision != 0 && (c.LPCPrecision < 4 || c.LPCPrecision > 15) {
		return fmt.Errorf("FLAC LPC precision must be between 4 and 15: %d", c.LPCPrecision)
	}
	if !validStereoMode(c.StereoMode) {
		return fmt.Errorf("invalid FLAC stereo mode: %q", c.StereoMode)
	}
	if !validOrderSearchMode(c.FixedOrderSearch) || !validOrderSearchMode(c.LPCOrderSearch) {
		return fmt.Errorf("invalid FLAC encoder order search mode")
	}
	if !validRiceCostMode(c.RiceCost) {
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
	if !validBlockSplitMode(c.BlockSplitMode) {
		return fmt.Errorf("invalid FLAC block split mode: %q", c.BlockSplitMode)
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

func validStereoMode(value StereoMode) bool {
	return value == "" || value == StereoIndependent || value == StereoAdaptive || value == StereoExhaustive
}
func validOrderSearchMode(value OrderSearchMode) bool {
	return value == "" || value == OrderSearchEstimated || value == OrderSearchExhaustive
}
func validRiceCostMode(value RiceCostMode) bool {
	return value == "" || value == RiceCostEstimated || value == RiceCostExact
}
func validBlockSplitMode(value BlockSplitMode) bool {
	return value == "" || value == BlockSplitEstimated || value == BlockSplitExact
}

func MergeEncoderConfigForFactory(cfg EncoderConfig, stream media.StreamInfo) EncoderConfig {
	if cfg.sampleRate == 0 && stream.Audio.SampleRate > 0 {
		cfg.sampleRate = stream.Audio.SampleRate
	}
	if cfg.channels == 0 {
		cfg.channels = stream.Audio.ChannelCount()
	}
	if cfg.bitsPerSample == 0 {
		cfg.bitsPerSample = stream.Audio.EffectiveBitsPerSample()
		if cfg.bitsPerSample == 0 {
			cfg.bitsPerSample = BitDepthFromSampleFormat(stream.Audio.Format)
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
