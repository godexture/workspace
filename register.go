package flac

import (
	"github.com/godexture/codec-flac/internal"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/engine"
)

type DecoderConfig = internal.DecoderConfig

func DefaultDecoderConfig() DecoderConfig { return internal.DefaultDecoderConfig() }

func NewDecoderConfigFromStreamInfo(stream media.StreamInfo) DecoderConfig {
	return internal.NewDecoderConfigFromStreamInfo(stream)
}

func NewDecoderEngine(config DecoderConfig) engine.DecoderEngine {
	return internal.NewDecoder(config)
}

type flacCapability struct{}

func (flacCapability) MediaType() media.MediaType { return media.MediaAudio }

func (c flacCapability) Match(streamInfo media.StreamInfo) bool {
	return streamInfo.Type == media.MediaAudio &&
		streamInfo.MediaAttributes.Codec == media.CodecFLAC
}

func (c flacCapability) Diagnose(streamInfo media.StreamInfo) bool {
	return c.Match(streamInfo)
}

func init() {
	if err := godec.Register(
		internal.DefaultDecoderConfig(),
		registry.DecoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        "flac-decoder",
					Description: "FLAC decoder",
				},
				Capabilities: []manifest.Capability{flacCapability{}},
				TransformFunc: func(streamInfo media.StreamInfo) media.Profile {
					profile := media.Profile{
						Type:            streamInfo.Type,
						MediaAttributes: streamInfo.MediaAttributes,
					}
					profile.Codec = media.CodecLPCM
					if profile.Audio.Format == media.SampleFormatUnknown {
						profile.Audio.Format = media.SampleFormatS16
					}
					return profile
				},
			},
			Factory: func(stream media.StreamInfo, config registry.Configuration) (node.Decoder, error) {
				decoderConfig, ok := config.(internal.DecoderConfig)
				if !ok {
					if decoderConfigPtr, ptrOK := config.(*internal.DecoderConfig); ptrOK && decoderConfigPtr != nil {
						decoderConfig = *decoderConfigPtr
					} else {
						decoderConfig = internal.NewDecoderConfigFromStreamInfo(stream)
					}
				}
				return engine.WrapDecoder(internal.NewDecoder(decoderConfig)), nil
			},
		},
	); err != nil {
		panic(err)
	}
}
