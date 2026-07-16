package flac

import (
	"github.com/godexture/codec-flac/internal/flac"
	"github.com/godexture/sdk/config"
	"github.com/godexture/sdk/optional"
)

type EncoderConfig struct {
	SampleRate    optional.Optional[int]
	Channels      optional.Optional[int]
	BitsPerSample optional.Optional[int]

	BlockSize                 optional.Optional[int]
	MaxFixedOrder             optional.Optional[int]
	MaxLPCOrder               optional.Optional[int]
	MaxRicePartitionOrder     optional.Optional[int]
	EnableWastedBits          optional.Optional[bool]
	EnableStereoDecorrelation optional.Optional[bool]
	EnableExhaustiveSearch    optional.Optional[bool]
	BlockingStrategy          optional.Optional[flac.BlockingStrategy]
	StreamableSubset          optional.Optional[bool]
}

func (EncoderConfig) NodeConfiguration() {}

func (c EncoderConfig) ApplyDefaults() flac.EncoderConfig {
	return config.ApplyDefaults(c, flac.DefaultEncoderConfig)
}
