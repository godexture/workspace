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

func NewDecoderEngine(stream media.StreamInfo, config DecoderConfig) engine.DecoderEngine {
	return internal.NewDecoder(stream, config)
}

func NewEncoderEngine(config EncoderConfig) (engine.EncoderEngine, error) {
	return internal.NewEncoder(config.ApplyDefaults())
}

type flacCapability struct{}

type lpcmCapability struct{}

func (flacCapability) MediaType() media.MediaType { return media.MediaAudio }

func (c flacCapability) Match(streamInfo media.StreamInfo) bool {
	return streamInfo.Type == media.MediaAudio &&
		streamInfo.MediaAttributes.Codec == media.CodecFLAC
}

func (c flacCapability) Diagnose(streamInfo media.StreamInfo) bool {
	return c.Match(streamInfo)
}

func (lpcmCapability) MediaType() media.MediaType { return media.MediaAudio }

func (c lpcmCapability) Match(streamInfo media.StreamInfo) bool {
	return streamInfo.Type == media.MediaAudio &&
		streamInfo.MediaAttributes.Codec == media.CodecLPCM
}

func (c lpcmCapability) Diagnose(streamInfo media.StreamInfo) bool {
	return c.Match(streamInfo)
}

func init() {
	if err := godec.Register(
		internal.DecoderConfig{},
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
						decoderConfig = internal.DecoderConfig{}
					}
				}
				return engine.WrapDecoder(internal.NewDecoder(stream, decoderConfig)), nil
			},
		},
	); err != nil {
		panic(err)
	}

	if err := godec.Register(
		EncoderConfig{},
		registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        "flac-encoder",
					Description: "FLAC encoder",
				},
				Capabilities: []manifest.Capability{lpcmCapability{}},
				TransformFunc: func(streamInfo media.StreamInfo) media.Profile {
					profile := media.Profile{
						Type:            streamInfo.Type,
						MediaAttributes: streamInfo.MediaAttributes,
					}
					profile.Codec = media.CodecFLAC
					return profile
				},
			},
			Supports: func(codec media.CodecID) bool {
				return codec == media.CodecFLAC
			},
			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, cfg registry.Configuration) (node.Encoder, error) {
				if cfg != nil {
					var resolved internal.EncoderConfig
					if flacConfig, ok := cfg.(EncoderConfig); ok {
						resolved = flacConfig.ApplyDefaults()
					} else if flacConfigPtr, ok := cfg.(*EncoderConfig); ok && flacConfigPtr != nil {
						resolved = flacConfigPtr.ApplyDefaults()
					}
					resolved = internal.MergeEncoderConfigForFactory(resolved, inStream)
					encoder, err := internal.NewEncoder(resolved)
					if err != nil {
						return nil, err
					}
					return engine.WrapEncoder(encoder), nil
				}
				resolved := internal.MergeEncoderConfigForFactory(internal.DefaultEncoderConfig, inStream)
				encoder, err := internal.NewEncoder(resolved)
				if err != nil {
					return nil, err
				}
				return engine.WrapEncoder(encoder), nil
			},
		},
	); err != nil {
		panic(err)
	}
}
