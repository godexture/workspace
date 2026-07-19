package flac

//go:generate go run ../../tools/config-generator -target EncoderConfig -target DecoderConfig

import (
	"github.com/godexture/codec-flac/internal/config"
)

type StereoMode = config.StereoMode
type BlockSplitMode = config.BlockSplitMode
type OrderSearchMode = config.OrderSearchMode
type RiceCostMode = config.RiceCostMode

const (
	StereoIndependent     = config.StereoIndependent
	StereoAdaptive        = config.StereoAdaptive
	StereoExhaustive      = config.StereoExhaustive
	BlockSplitEstimated   = config.BlockSplitEstimated
	BlockSplitExact       = config.BlockSplitExact
	OrderSearchEstimated  = config.OrderSearchEstimated
	OrderSearchExhaustive = config.OrderSearchExhaustive
	RiceCostEstimated     = config.RiceCostEstimated
	RiceCostExact         = config.RiceCostExact
)


