package flac

//go:generate go run ../../tools/config-generator -source=internal/config/encoder.go -type=EncoderConfig -resolved-type=config.EncoderConfig -default=config.DefaultEncoderConfig -preset=config.GetPreset -preset-normalizer=normalizeCompressionLevel -import=config=github.com/godexture/codec-flac/internal/config -output=config_encoder.go
//go:generate go run ../../tools/config-generator -source=internal/config/decoder.go -type=DecoderConfig -resolved-type=config.DecoderConfig -default=config.DefaultDecoderConfig -import=config=github.com/godexture/codec-flac/internal/config -output=config_decoder.go

import (
	"log"

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

func normalizeCompressionLevel(level int) int {
	if level < 0 {
		log.Printf("WARNING: FLAC compression level %d is outside 0..8; using 0", level)
		return 0
	}
	if level > 8 {
		log.Printf("WARNING: FLAC compression level %d is outside 0..8; using 8", level)
		return 8
	}
	return level
}
