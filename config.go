package flac

//go:generate go run ../../tools/config-generator -source=internal/flac/config.go -type=EncoderConfig -resolved-type=flac.EncoderConfig -default=flac.GetPreset(5) -preset=flac.GetPreset -preset-normalizer=normalizeCompressionLevel -import=flac=github.com/godexture/codec-flac/internal/flac -output=config_encoder.go
//go:generate go run ../../tools/config-generator -source=internal/flac/config.go -type=DecoderConfig -resolved-type=flac.DecoderConfig -import=flac=github.com/godexture/codec-flac/internal/flac -output=config_decoder.go

import (
	"log"

	"github.com/godexture/codec-flac/internal/flac"
)

func (DecoderConfig) NodeConfiguration() {}
func (EncoderConfig) NodeConfiguration() {}

const (
	StereoIndependent = flac.StereoIndependent
	StereoAdaptive    = flac.StereoAdaptive
	StereoExhaustive  = flac.StereoExhaustive
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
