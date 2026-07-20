package mp3

import (
	"github.com/godexture/codec-mp3/internal"
	domain "github.com/godexture/codec-mp3/internal/domain"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/engine"
)

func NewDecoderEngine(config DecoderConfig) engine.DecoderEngine {
	return internal.NewDecoder()
}

func NewEncoderEngine(config EncoderConfig) (engine.EncoderEngine, error) {
	resolved, err := engine.ResolveConfig[domain.EncoderConfig, EncoderConfig](config)
	if err != nil {
		return nil, err
	}
	return internal.NewEncoder(resolved), nil
}

func init() {
	if err := godec.Register(NewDecoderConfig(), registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest: registry.BaseManifest{
				Name:        "mp3-decoder",
				Description: "MP3 decoder",
			},
			Capabilities: []manifest.Capability{&manifest.AudioConstraint{Codecs: []media.CodecID{media.CodecMP3}}},
			TransformFunc: func(stream media.StreamInfo, _ media.CodecID, _ registry.Configuration) (media.Profile, error) {
				profile := media.Profile{
					Type:            stream.Type,
					MediaAttributes: stream.MediaAttributes,
				}
				profile.Codec = media.CodecLPCM
				if profile.Audio.Format == media.SampleFormatUnknown {
					profile.Audio.Format = media.SampleFormatF32
				}
				if stream.Audio.ChannelCount() == 1 {
					profile.Audio.ChannelLayout = media.LayoutMono1
				} else {
					profile.Audio.ChannelLayout = media.LayoutStereo2_0
				}
				return profile, nil
			},
		},
		Factory: func(s media.StreamInfo, options registry.TransformFactoryOptions) (node.Decoder, error) {
			return engine.WrapDecoder(internal.NewDecoder()), nil
		},
	}); err != nil {
		panic(err)
	}

	if err := godec.Register(NewEncoderConfig(), registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest: registry.BaseManifest{
				Name:        "mp3-encoder",
				Description: "MP3 encoder (codec-mp3 plugin)",
			},
			Capabilities: []manifest.Capability{&manifest.AudioConstraint{Codecs: []media.CodecID{media.CodecLPCM}}},
			TransformFunc: func(stream media.StreamInfo, target media.CodecID, _ registry.Configuration) (media.Profile, error) {
				profile := media.Profile{Type: stream.Type, MediaAttributes: stream.MediaAttributes}
				profile.Codec = target
				return profile, nil
			},
		},
		Supports: func(codec media.CodecID) bool {
			return codec == media.CodecMP3
		},
		Factory: func(s media.StreamInfo, targetCodec media.CodecID, options registry.TransformFactoryOptions) (node.Encoder, error) {
			resolved, err := engine.ResolveConfig[domain.EncoderConfig, EncoderConfig](options.Config)
			if err != nil {
				return nil, err
			}
			return engine.WrapEncoder(internal.NewEncoder(resolved)), nil
		},
	}); err != nil {
		panic(err)
	}
}
