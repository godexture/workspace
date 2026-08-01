package mp3

import (
	"github.com/godexture/godec/plugin/mp3/internal/codec"
	godec "github.com/godexture/godec/core"
	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/sdk/engine"
)

func NewDecoderEngine(config DecoderConfig) engine.DecoderEngine {
	return internal.NewDecoder()
}

func init() {
	if err := godec.Register(registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest: registry.BaseManifest{
				Name:                 "mp3",
				Description:          "MP3 decoder",
				ConfigurationFactory: registry.NewConfigurationFactory(NewDecoderConfig),
			},
			InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(&manifest.AudioConstraint{Codecs: []media.CodecID{media.CodecMP3}})),
		},
		Factory: func(stream media.StreamInfo, _ registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
			output := stream.Clone()
			output.Codec = media.CodecLPCM
			if output.Audio.Format == media.SampleFormatUnknown {
				output.Audio.Format = media.SampleFormatF32
			}
			if stream.Audio.ChannelCount() == 1 {
				output.Audio.ChannelLayout = media.LayoutMono1
			} else {
				output.Audio.ChannelLayout = media.LayoutStereo2_0
			}
			return engine.WrapDecoder(internal.NewDecoder()), output, nil
		},
	}); err != nil {
		panic(err)
	}

}
