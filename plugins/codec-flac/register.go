package flac

import (
	"github.com/godexture/codec-flac/internal/decoder"
	"github.com/godexture/codec-flac/internal/encoder"
	"github.com/godexture/codec-flac/internal/flac"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/engine"
)

type DecoderConfig = flac.DecoderConfig

func NewDecoderEngine(stream media.StreamInfo, config DecoderConfig) engine.DecoderEngine {
	return decoder.NewDecoder(stream, config)
}

func NewEncoderEngine(config EncoderConfig) (engine.EncoderEngine, error) {
	return encoder.NewEncoder(config.ApplyDefaults())
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
		flac.DecoderConfig{},
		registry.DecoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        "flac-decoder",
					Description: "FLAC decoder",
				},
				Capabilities: []manifest.Capability{flacCapability{}},
				TransformFunc: func(streamInfo media.StreamInfo, _ media.CodecID, _ registry.Configuration) (media.Profile, error) {
					profile := media.Profile{
						Type:            streamInfo.Type,
						MediaAttributes: streamInfo.MediaAttributes,
					}
					profile.Codec = media.CodecLPCM
					if profile.Audio.Format == media.SampleFormatUnknown {
						profile.Audio.Format = media.SampleFormatS16
					}
					return profile, nil
				},
			},
			Factory: func(stream media.StreamInfo, config registry.Configuration) (node.Decoder, error) {
				decoderConfig, ok := config.(flac.DecoderConfig)
				if !ok {
					if decoderConfigPtr, ptrOK := config.(*flac.DecoderConfig); ptrOK && decoderConfigPtr != nil {
						decoderConfig = *decoderConfigPtr
					} else {
						decoderConfig = flac.DecoderConfig{}
					}
				}
				return engine.WrapDecoder(decoder.NewDecoder(stream, decoderConfig)), nil
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
				TransformFunc: func(streamInfo media.StreamInfo, target media.CodecID, _ registry.Configuration) (media.Profile, error) {
					profile := media.Profile{
						Type:            streamInfo.Type,
						MediaAttributes: streamInfo.MediaAttributes,
					}
					profile.Codec = target
					return profile, nil
				},
			},
			Supports: func(codec media.CodecID) bool {
				return codec == media.CodecFLAC
			},
			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, cfg registry.Configuration) (node.Encoder, error) {
				if cfg != nil {
					var resolved flac.EncoderConfig
					if flacConfig, ok := cfg.(EncoderConfig); ok {
						resolved = flacConfig.ApplyDefaults()
					} else if flacConfigPtr, ok := cfg.(*EncoderConfig); ok && flacConfigPtr != nil {
						resolved = flacConfigPtr.ApplyDefaults()
					}
					resolved = flac.MergeEncoderConfigForFactory(resolved, inStream)
					enc, err := encoder.NewEncoder(resolved)
					if err != nil {
						return nil, err
					}
					return engine.WrapEncoder(enc), nil
				}
				resolved := flac.MergeEncoderConfigForFactory(flac.DefaultEncoderConfig, inStream)
				enc, err := encoder.NewEncoder(resolved)
				if err != nil {
					return nil, err
				}
				return engine.WrapEncoder(enc), nil
			},
		},
	); err != nil {
		panic(err)
	}
}
