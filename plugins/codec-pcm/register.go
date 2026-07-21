package pcm

import (
	internal "github.com/godexture/codec-pcm/internal"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/format-wav/params"
	"github.com/godexture/sdk/engine"
)

func NewDecoderEngine(stream media.StreamInfo, cfg DecoderConfig) (engine.DecoderEngine, error) {
	resolved, err := engine.ResolveConfig[internal.DecoderConfig, DecoderConfig](cfg)
	if err != nil {
		return nil, err
	}
	return internal.NewDecoder(stream, resolved)
}

func NewEncoderEngine(stream media.StreamInfo, cfg EncoderConfig) engine.EncoderEngine {
	resolved, err := engine.ResolveConfig[internal.EncoderConfig, EncoderConfig](cfg)
	if err != nil {
		panic(err)
	}
	enc, _ := internal.NewEncoder(stream, resolved.CodecID, resolved)
	return enc
}

func init() {
	// --- Decoder ---
	if err := godec.Register(registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest: registry.BaseManifest{
				Name:                 "pcm",
				Description:          "LPCM/G.711/ADPCM decoder",
				ConfigurationFactory: registry.NewConfigurationFactory(NewDecoderConfig),
			},
			InputRequirements: registry.StaticRequirements(&manifest.AudioConstraint{Codecs: []media.CodecID{
				media.CodecLPCM, media.CodecPCMU, media.CodecPCMA, media.CodecMSADPCM, media.CodecIMAADPCM,
			}}),
			TransformFunc: func(s media.StreamInfo, _ media.CodecID, _ registry.Configuration) (media.Profile, error) {
				p := media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
				p.Codec = media.CodecLPCM
				p.Audio = internal.GetDecodedAttributes(s.Codec, s.Audio)
				return p, nil
			},
		},
		Factory: func(s media.StreamInfo, options registry.TransformFactoryOptions) (node.Decoder, error) {
			resolved, err := engine.ResolveConfig[internal.DecoderConfig, DecoderConfig](options.Config)
			if err != nil {
				return nil, err
			}
			decoder, err := internal.NewDecoder(s, resolved)
			if err != nil {
				return nil, err
			}
			return engine.WrapDecoder(decoder), nil
		},
	}); err != nil {
		panic(err)
	}

	// --- Encoder ---
	if err := godec.Register(registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest: registry.BaseManifest{
				Name:                 "pcm",
				Description:          "LPCM/G.711/ADPCM encoder",
				ConfigurationFactory: registry.NewConfigurationFactory(NewEncoderConfig),
			},
			InputRequirements: registry.StaticRequirements(&manifest.AudioConstraint{Codecs: []media.CodecID{
				media.CodecLPCM, media.CodecPCMU, media.CodecPCMA, media.CodecMSADPCM, media.CodecIMAADPCM,
			}}),
			TransformFunc: func(in media.StreamInfo, target media.CodecID, cfg registry.Configuration) (media.Profile, error) {
				resolved, err := engine.ResolveConfig[internal.EncoderConfig, EncoderConfig](cfg)
				if err != nil {
					return media.Profile{}, err
				}
				profile := media.Profile{Type: in.Type, MediaAttributes: in.MediaAttributes}
				profile.Codec = target
				if target == media.CodecMSADPCM || target == media.CodecIMAADPCM {
					channels := profile.Audio.ChannelLayout.ChannelCount()
					if channels == 0 {
						channels = 1
					}

					adpcm := resolved.ADPCM
					if adpcm.BlockAlign == 0 {
						if in.Codec == target && media.IsCodecParameters[params.ADPCM](in.CodecParameters) {
							if p, err := params.Parse(target, in.Audio.ChannelLayout.ChannelCount(), in.CodecParameters.Data); err == nil {
								adpcm = p
							}
						}
					}
					if adpcm.BlockAlign == 0 {
						adpcm, _ = params.Default(target, channels)
					}

					if err := adpcm.Validate(target, channels); err != nil {
						return media.Profile{}, err
					}
					profile.CodecParameters = media.NewCodecParameters[params.ADPCM](adpcm.MarshalBinary())
				} else {
					profile.CodecParameters = media.CodecParameters{}
				}
				return profile, nil
			},
		},
		Codecs: []media.CodecID{media.CodecLPCM, media.CodecPCMU, media.CodecPCMA, media.CodecMSADPCM, media.CodecIMAADPCM},
		Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, options registry.TransformFactoryOptions) (node.Encoder, error) {
			resolved, err := engine.ResolveConfig[internal.EncoderConfig, EncoderConfig](options.Config)
			if err != nil {
				return nil, err
			}
			enc, err := internal.NewEncoder(inStream, targetCodec, resolved)
			if err != nil {
				return nil, err
			}
			return engine.WrapEncoder(enc), nil
		},
	}); err != nil {
		panic(err)
	}
}
