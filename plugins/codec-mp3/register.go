package mp3

import (
	"github.com/godexture/codec-mp3/internal"
	"github.com/godexture/codec-mp3/internal/domain"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/engine"
)

type DecoderConfig = domain.DecoderConfig
type EncoderConfig = domain.EncoderConfig

func NewDecoderEngine(config DecoderConfig) engine.DecoderEngine {
	return internal.NewDecoder()
}

func NewEncoderEngine(config EncoderConfig) engine.EncoderEngine {
	return internal.NewEncoder(config)
}

type mp3Capability struct{}

func (mp3Capability) MediaType() media.MediaType { return media.MediaAudio }

func (c mp3Capability) Match(streamInfo media.StreamInfo) bool {
	return streamInfo.Type == media.MediaAudio &&
		streamInfo.MediaAttributes.Codec == media.CodecMP3
}

func (c mp3Capability) Diagnose(streamInfo media.StreamInfo) bool {
	return c.Match(streamInfo)
}

func init() {
	// --- Decoder ---
	if err := godec.Register(
		domain.DecoderConfig{},
		registry.DecoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        "mp3-decoder",
					Description: "MP3 decoder",
				},

				Capabilities: []manifest.Capability{mp3Capability{}},

				TransformFunc: func(streamInfo media.StreamInfo) media.Profile {
					profile := media.Profile{
						Type:            streamInfo.Type,
						MediaAttributes: streamInfo.MediaAttributes,
					}
					profile.Codec = media.CodecLPCM
					profile.Audio.Format = media.SampleFormatF32

					if streamInfo.Audio.ChannelCount() == 1 {
						profile.Audio.ChannelLayout = media.LayoutMono1
					} else {
						profile.Audio.ChannelLayout = media.LayoutStereo2_0
					}

					return profile
				},
			},

			Factory: func(stream media.StreamInfo, config registry.Configuration) (node.Decoder, error) {
				return engine.WrapDecoder(internal.NewDecoder()), nil
			},
		},
	); err != nil {
		panic(err)
	}

	// --- Encoder (stub) ---
	if err := godec.Register(
		domain.EncoderConfig{},
		registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        "mp3-encoder",
					Description: "MP3 encoder (codec-mp3 plugin) [STUB: 未実装]",
				},
				Capabilities: []manifest.Capability{mp3Capability{}},
				TransformFunc: func(streamInfo media.StreamInfo) media.Profile {
					profile := media.Profile{Type: streamInfo.Type, MediaAttributes: streamInfo.MediaAttributes}
					profile.Codec = media.CodecMP3
					return profile
				},
			},

			Supports: func(codec media.CodecID) bool {
				return codec == media.CodecMP3
			},

			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, config registry.Configuration) (node.Encoder, error) {
				encoderConfig := domain.EncoderConfig{}
				return engine.WrapEncoder(internal.NewEncoder(encoderConfig)), nil
			},
		},
	); err != nil {
		panic(err)
	}
}
