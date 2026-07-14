package mp3

import (
	"github.com/godexture/codec-mp3/internal/domain"
	"github.com/godexture/sdk/config"
	"github.com/godexture/sdk/optional"
)

type EncoderConfig struct {
	Bitrate optional.Optional[int]
}

func (EncoderConfig) NodeConfiguration() {}

func (c EncoderConfig) ApplyDefaults() domain.EncoderConfig {
	return config.ApplyDefaults(c, domain.DefaultEncoderConfig)
}
