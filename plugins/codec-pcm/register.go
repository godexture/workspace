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

func NewDecoderEngine(stream media.StreamInfo, cfg DecoderConfig) engine.DecoderEngine {
	resolved, err := engine.ResolveConfig[internal.DecoderConfig, DecoderConfig](cfg)
	if err != nil {
		panic(err)
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

type pcmCapability struct {
	codec media.CodecID
}

func (pcmCapability) MediaType() media.MediaType { return media.MediaAudio }

func (c pcmCapability) Match(stream media.StreamInfo) bool {
	return stream.Type == media.MediaAudio && stream.MediaAttributes.Codec == c.codec
}

func (c pcmCapability) Diagnose(stream media.StreamInfo) bool {
	return c.Match(stream)
}

func init() {
	// --- Decoder ---
	if err := godec.Register(NewDecoderConfig(), registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest: registry.BaseManifest{
				Name:        "pcm-decoder",
				Description: "PCM/G.711 decoder",
			},
			Capabilities: []manifest.Capability{
				pcmCapability{codec: media.CodecLPCM},
				pcmCapability{codec: media.CodecPCMU},
				pcmCapability{codec: media.CodecPCMA},
				pcmCapability{codec: media.CodecMSADPCM},
				pcmCapability{codec: media.CodecIMAADPCM},
			},
			TransformFunc: func(s media.StreamInfo, _ media.CodecID, _ registry.Configuration) (media.Profile, error) {
				p := media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
				p.Audio = internal.GetDecodedAttributes(s.Codec, s.Audio)
				return p, nil
			},
		},
		Factory: func(s media.StreamInfo, cfg registry.Configuration) (node.Decoder, error) {
			resolved, err := engine.ResolveConfig[internal.DecoderConfig, DecoderConfig](cfg)
			if err != nil {
				return nil, err
			}
			return engine.WrapDecoder(internal.NewDecoder(s, resolved)), nil
		},
	}); err != nil {
		panic(err)
	}

	// --- Encoder ---
	if err := godec.Register(NewEncoderConfig(), registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest: registry.BaseManifest{
				Name:        "pcm-encoder",
				Description: "PCM/G.711 encoder",
			},
			Capabilities: []manifest.Capability{
				pcmCapability{codec: media.CodecLPCM},
				pcmCapability{codec: media.CodecPCMU},
				pcmCapability{codec: media.CodecPCMA},
				pcmCapability{codec: media.CodecMSADPCM},
				pcmCapability{codec: media.CodecIMAADPCM},
			},
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
		Supports: func(codec media.CodecID) bool {
			return codec == media.CodecLPCM || codec == media.CodecPCMU || codec == media.CodecPCMA || codec == media.CodecMSADPCM || codec == media.CodecIMAADPCM
		},
		Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, cfg registry.Configuration) (node.Encoder, error) {
			resolved, err := engine.ResolveConfig[internal.EncoderConfig, EncoderConfig](cfg)
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
