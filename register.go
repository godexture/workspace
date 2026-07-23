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
	pool := execution.newOwnedPool()
	dec := decoder.NewDecoder(stream, resolved, pool)
	if pool == nil {
		return dec, nil
	}
	return &ownedPoolDecoderEngine{Decoder: dec, pool: pool}, nil
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
	pool := execution.newOwnedPool()
	enc := encoder.NewEncoder(media.StreamInfo{}, resolved, pool)
	if pool == nil {
		return enc, nil
	}
	return &ownedPoolEncoderEngine{Encoder: enc, pool: pool}, nil
}

func init() {
	if err := godec.Register(
		registry.DecoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:                 "flac",
					Description:          "FLAC decoder",
					ConfigurationFactory: registry.NewConfigurationFactory(NewDecoderConfig),
				},
				InputRequirements: registry.StaticRequirements(&manifest.AudioConstraint{Codecs: []media.CodecID{media.CodecFLAC}}),
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
				return engine.WrapDecoder(decoder.NewDecoder(stream, resolved, options.Resources.Pool)), nil
			},
		},
	); err != nil {
		panic(err)
	}

	if err := godec.Register(
		registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:                 "flac",
					Description:          "FLAC encoder",
					ConfigurationFactory: registry.NewConfigurationFactory(NewEncoderConfig),
				},
				InputRequirements: registry.StaticRequirements(&manifest.AudioConstraint{
					Codecs: []media.CodecID{media.CodecLPCM},
					// Max is the FLAC Subset format limit: the largest rate a frame
					// header can still encode explicitly (10Hz-unit field, 16 bits).
					// The raw STREAMINFO field allows up to 2^20-1, but Subset streams
					// (the default) require an explicit per-frame sample-rate code.
					SampleRates: manifest.IntConstraint{Min: 1, Max: 655350},
					Channels:    manifest.IntConstraint{Min: 1, Max: 8},
					SampleFormats: []manifest.SampleFormatConstraint{
						{Format: media.SampleFormatS16, BitsPerSample: manifest.IntConstraint{Min: 4, Max: 16}},
						{Format: media.SampleFormatS24, BitsPerSample: manifest.IntConstraint{Min: 17, Max: 24}},
						{Format: media.SampleFormatS32, BitsPerSample: manifest.IntConstraint{Min: 17, Max: 32}},
					},
				}),
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
			Codecs: []media.CodecID{media.CodecFLAC},
			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, options registry.TransformFactoryOptions) (node.Encoder, error) {
				resolved, err := engine.ResolveConfig[config.EncoderConfig, EncoderConfig](options.Config)
				if err != nil {
					return nil, err
				}
				enc := encoder.NewEncoder(inStream, resolved, options.Resources.Pool)
				return engine.WrapEncoder(enc), nil
			},
		},
	); err != nil {
		panic(err)
	}
}
