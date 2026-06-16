package mp3codec

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
		streamInfo.MediaAttributes.Codec == media.CodecMPEG3
}

func (c mp3Capability) Diagnose(streamInfo media.StreamInfo) bool {
	return c.Match(streamInfo)
}

func init() {
	// --- Decoder ---
	if err := godec.DefaultRegistry.Decoders.Register(
		domain.DecoderConfig{},
		registry.DecoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        "mp3-decoder",
					Description: "MP3 decoder (codec-mp3 plugin, custom minimp3 backend)",
				},
				Capabilities: []manifest.Capability{mp3Capability{}},
				TransformFunc: func(streamInfo media.StreamInfo) media.Profile {
					profile := media.Profile{Type: streamInfo.Type, MediaAttributes: streamInfo.MediaAttributes}
					profile.Audio.CodecID = media.CodecLPCM // デコード後はPCM
					profile.Audio.Format = media.SampleFormatS16
					if streamInfo.Audio.ChannelCount() == 1 {
						profile.Audio.ChannelLayout = media.LayoutMono1
					} else {
						profile.Audio.ChannelLayout = media.LayoutStereo2_0
					}
					return profile
				},
			},
			Factory: func(config registry.Configuration) (node.Decoder, error) {
				return engine.WrapDecoder(internal.NewDecoder()), nil
			},
		},
	); err != nil {
		panic(err)
	}

	// --- Encoder (stub) ---
	if err := godec.DefaultRegistry.Encoders.Register(
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
					profile.Audio.CodecID = media.CodecMPEG3
					return profile
				},
			},
			Supports: func(codec media.CodecID) bool {
				return codec == media.CodecMPEG3
			},
			Factory: func(config registry.Configuration) (node.Encoder, error) {
				encoderConfig := domain.EncoderConfig{}
				return engine.WrapEncoder(internal.NewEncoder(encoderConfig)), nil
			},
		},
	); err != nil {
		panic(err)
	}
}
