package pcm

import (
	"encoding/binary"

	internal "github.com/godexture/codec-pcm/internal"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/format-wav/params"
	"github.com/godexture/sdk/config"
	"github.com/godexture/sdk/optional"
)

type DecoderConfig struct {
	CodecID       optional.Optional[media.CodecID]
	SampleRate    optional.Optional[int]
	Format        optional.Optional[media.SampleFormat]
	ChannelLayout optional.Optional[media.ChannelLayout]
	ByteOrder     optional.Optional[binary.ByteOrder]
	ADPCM         optional.Optional[params.ADPCM]
}

func (DecoderConfig) NodeConfiguration() {}

func (c DecoderConfig) ApplyDefaults() internal.DecoderConfig {
	return config.ApplyDefaults(c, internal.DefaultDecoderConfig)
}

type EncoderConfig struct {
	CodecID   optional.Optional[media.CodecID]
	ByteOrder optional.Optional[binary.ByteOrder]
	// ADPCM overrides the output ADPCM framing parameters. It must be a valid
	// params.ADPCM value for the selected codec and channel layout.
	ADPCM optional.Optional[params.ADPCM]
}

func (EncoderConfig) NodeConfiguration() {}

func (c EncoderConfig) ApplyDefaults() internal.EncoderConfig {
	return config.ApplyDefaults(c, internal.DefaultEncoderConfig)
}
