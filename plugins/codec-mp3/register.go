package mp3

import (
	"github.com/godexture/codec-mp3/internal"
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

func NewEncoderEngine(config EncoderConfig) engine.EncoderEngine {
	resolved := engine.ResolveConfig[EncoderConfig](config)
	return internal.NewEncoder(resolved)
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

type lpcmCapability struct{}

func (lpcmCapability) MediaType() media.MediaType { return media.MediaAudio }

func (c lpcmCapability) Match(streamInfo media.StreamInfo) bool {
	return streamInfo.Type == media.MediaAudio &&
		streamInfo.MediaAttributes.Codec == media.CodecLPCM
}

func (c lpcmCapability) Diagnose(streamInfo media.StreamInfo) bool {
	return c.Match(streamInfo)
}

func init() {
	if err := godec.Register(DecoderConfig{}, registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest: registry.BaseManifest{
				Name:        "mp3-decoder",
				Description: "MP3 decoder",
			},
			Capabilities: []manifest.Capability{
				mp3Capability{},
			},
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
		Factory: func(s media.StreamInfo, cfg registry.Configuration) (node.Decoder, error) {
			return engine.WrapDecoder(internal.NewDecoder()), nil
		},
	}); err != nil {
		panic(err)
	}

	if err := godec.Register(EncoderConfig{}, registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest: registry.BaseManifest{
				Name:        "mp3-encoder",
				Description: "MP3 encoder (codec-mp3 plugin)",
			},
			Capabilities: []manifest.Capability{
				lpcmCapability{},
			},
			TransformFunc: func(stream media.StreamInfo, target media.CodecID, _ registry.Configuration) (media.Profile, error) {
				profile := media.Profile{Type: stream.Type, MediaAttributes: stream.MediaAttributes}
				profile.Codec = target
				return profile, nil
			},
		},
		Supports: func(codec media.CodecID) bool {
			return codec == media.CodecMP3
		},
		Factory: func(s media.StreamInfo, targetCodec media.CodecID, cfg registry.Configuration) (node.Encoder, error) {
			resolved := engine.ResolveConfig[EncoderConfig](cfg)
			return engine.WrapEncoder(internal.NewEncoder(resolved)), nil
		},
	}); err != nil {
		panic(err)
	}
}
