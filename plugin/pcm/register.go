package pcm

import (
	godec "github.com/godexture/godec/core"
	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
	internal "github.com/godexture/godec/plugin/pcm/internal"
	"github.com/godexture/godec/plugin/wave/params"
	"github.com/godexture/godec/sdk/engine"
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
			InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(&manifest.AudioConstraint{Codecs: []media.CodecID{
				media.CodecLPCM, media.CodecPCMU, media.CodecPCMA, media.CodecMSADPCM, media.CodecIMAADPCM,
			}})),
		},
		Factory: func(s media.StreamInfo, options registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
			resolved, err := engine.ResolveConfig[internal.DecoderConfig, DecoderConfig](options.Config)
			if err != nil {
				return nil, media.StreamInfo{}, err
			}
			decoder, err := internal.NewDecoder(s, resolved)
			if err != nil {
				return nil, media.StreamInfo{}, err
			}
			output := s.Clone()
			output.Codec = media.CodecLPCM
			output.Audio = internal.GetDecodedAttributes(s.Codec, s.Audio)
			return engine.WrapDecoder(decoder), output, nil
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
			InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(&manifest.AudioConstraint{Codecs: []media.CodecID{
				media.CodecLPCM, media.CodecPCMU, media.CodecPCMA, media.CodecMSADPCM, media.CodecIMAADPCM,
			}})),
		},
		Codecs: []media.CodecID{media.CodecLPCM, media.CodecPCMU, media.CodecPCMA, media.CodecMSADPCM, media.CodecIMAADPCM},
		Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, options registry.TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
			resolved, err := engine.ResolveConfig[internal.EncoderConfig, EncoderConfig](options.Config)
			if err != nil {
				return nil, media.StreamInfo{}, err
			}
			enc, err := internal.NewEncoder(inStream, targetCodec, resolved)
			if err != nil {
				return nil, media.StreamInfo{}, err
			}
			profile := inStream.Clone()
			profile.Codec = targetCodec
			if targetCodec == media.CodecMSADPCM || targetCodec == media.CodecIMAADPCM {
				channels := profile.Audio.ChannelLayout.ChannelCount()
				if channels == 0 {
					channels = 1
				}

				adpcm := resolved.ADPCM
				if adpcm.BlockAlign == 0 {
					if inStream.Codec == targetCodec && media.IsCodecParameters[params.ADPCM](inStream.CodecParameters) {
						if p, err := params.Parse(targetCodec, inStream.Audio.ChannelLayout.ChannelCount(), inStream.CodecParameters.Data); err == nil {
							adpcm = p
						}
					}
				}
				if adpcm.BlockAlign == 0 {
					adpcm, _ = params.Default(targetCodec, channels)
				}

				if err := adpcm.Validate(targetCodec, channels); err != nil {
					return nil, media.StreamInfo{}, err
				}
				profile.CodecParameters = media.NewCodecParameters[params.ADPCM](adpcm.MarshalBinary())
			} else {
				profile.CodecParameters = media.CodecParameters{}
			}
			return engine.WrapEncoder(enc), profile, nil
		},
	}); err != nil {
		panic(err)
	}
}
