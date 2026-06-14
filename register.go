package mp3codec

import (
	internal "github.com/godexture/codec-mp3/internal"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/engine"
)

type DecoderConfig = internal.DecoderConfig
type EncoderConfig = internal.EncoderConfig

func NewDecoderEngine(config DecoderConfig) engine.DecoderEngine {
	return internal.NewDecoder()
}

func NewEncoderEngine(config EncoderConfig) engine.EncoderEngine {
	return internal.NewEncoder(config)
}

type mp3Capability struct{}

func (mp3Capability) MediaType() media.MediaType { return media.MediaAudio }

func (c mp3Capability) Match(stream media.StreamInfo) bool {
	return stream.Type == media.MediaAudio &&
		stream.MediaAttributes.Codec == media.CodecMPEG3
}

func (c mp3Capability) Diagnose(stream media.StreamInfo) bool {
	return c.Match(stream)
}

func init() {
	// --- Decoder ---
	if err := godec.DefaultRegistry.Decoders.Register(
		internal.DecoderConfig{},
		registry.DecoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        "mp3-decoder",
					Description: "MP3 decoder (codec-mp3 plugin, custom minimp3 backend)",
				},
				Capabilities: []manifest.Capability{mp3Capability{}},
				TransformFunc: func(s media.StreamInfo) media.Profile {
					p := media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
					p.Audio.CodecID = media.CodecLPCM // デコード後はPCM
					p.Audio.Format = media.SampleFormatS16
					if s.Audio.ChannelCount() == 1 {
						p.Audio.ChannelLayout = media.LayoutMono1
					} else {
						p.Audio.ChannelLayout = media.LayoutStereo2_0
					}
					return p
				},
			},
			Factory: func(cfg registry.Configuration) (node.Decoder, error) {
				return engine.WrapDecoder(internal.NewDecoder()), nil
			},
		},
	); err != nil {
		panic(err)
	}

	// --- Encoder (stub) ---
	if err := godec.DefaultRegistry.Encoders.Register(
		internal.EncoderConfig{},
		registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        "mp3-encoder",
					Description: "MP3 encoder (codec-mp3 plugin) [STUB: 未実装]",
				},
				Capabilities: []manifest.Capability{mp3Capability{}},
				TransformFunc: func(s media.StreamInfo) media.Profile {
					p := media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
					p.Audio.CodecID = media.CodecMPEG3
					return p
				},
			},
			Supports: func(codec media.CodecID) bool {
				return codec == media.CodecMPEG3
			},
			Factory: func(cfg registry.Configuration) (node.Encoder, error) {
				c := internal.EncoderConfig{}
				return engine.WrapEncoder(internal.NewEncoder(c)), nil
			},
		},
	); err != nil {
		panic(err)
	}
}
