package config

import (
	"log"

	"github.com/godexture/godec/plugins/codec-flac/internal/flac"
)

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
