package flac

import (
	"github.com/godexture/codec-flac/internal/config"
	"github.com/godexture/codec-flac/internal/decoder"
	"github.com/godexture/codec-flac/internal/encoder"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/engine"
)

func NewDecoderEngine(stream media.StreamInfo, cfg DecoderConfig, options ...EngineOption) (engine.DecoderEngine, error) {
	resolved, err := engine.ResolveConfig[config.DecoderConfig, DecoderConfig](cfg)
	if err != nil {
		return nil, err
	}
	execution, err := resolveEngineOptions(options)
	if err != nil {
		return nil, err
	}
	return decoder.NewDecoder(stream, resolved, execution.parallelism), nil
}

func NewEncoderEngine(cfg EncoderConfig, options ...EngineOption) (engine.EncoderEngine, error) {
	resolved, err := engine.ResolveConfig[config.EncoderConfig, EncoderConfig](cfg)
	if err != nil {
		return nil, err
	}
	execution, err := resolveEngineOptions(options)
	if err != nil {
		return nil, err
	}
	return encoder.NewEncoder(media.StreamInfo{}, resolved, execution.parallelism), nil
}

func init() {
	if err := godec.Register(
		NewDecoderConfig(),
		registry.DecoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        "flac-decoder",
					Description: "FLAC decoder",
				},
				Capabilities: []manifest.Capability{&manifest.AudioConstraint{Codecs: []media.CodecID{media.CodecFLAC}}},
				Resources: registry.ResourceRequest{
					Parallelism: true,
				},
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
			Factory: func(stream media.StreamInfo, options registry.TransformFactoryOptions) (node.Decoder, error) {
				resolved, err := engine.ResolveConfig[config.DecoderConfig, DecoderConfig](options.Config)
				if err != nil {
					return nil, err
				}
				return engine.WrapDecoder(decoder.NewDecoder(stream, resolved, options.Resources.Parallelism)), nil
			},
		},
	); err != nil {
		panic(err)
	}

	if err := godec.Register(
		NewEncoderConfig(),
		registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        "flac-encoder",
					Description: "FLAC encoder",
				},
				Capabilities: []manifest.Capability{&manifest.AudioConstraint{Codecs: []media.CodecID{media.CodecLPCM}}},
				Resources: registry.ResourceRequest{
					Parallelism: true,
				},
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
			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, options registry.TransformFactoryOptions) (node.Encoder, error) {
				resolved, err := engine.ResolveConfig[config.EncoderConfig, EncoderConfig](options.Config)
				if err != nil {
					return nil, err
				}
				enc := encoder.NewEncoder(inStream, resolved, options.Resources.Parallelism)
				return engine.WrapEncoder(enc), nil
			},
		},
	); err != nil {
		panic(err)
	}
}
