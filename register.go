package pcm

import (
	"encoding/binary"

	internal "github.com/godexture/codec-pcm/internal"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/format-wav/params"
	"github.com/godexture/sdk/config"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/optional"
)

func DefaultDecoderConfig() DecoderConfig {
	return DecoderConfig{
		CodecID:       optional.Some(media.CodecLPCM),
		SampleRate:    optional.Some(48000),
		Format:        optional.Some(media.SampleFormatS16),
		ChannelLayout: optional.Some(media.LayoutStereo2_0),
		ByteOrder:     optional.Some[binary.ByteOrder](binary.LittleEndian),
	}
}

func DefaultEncoderConfig() EncoderConfig {
	return EncoderConfig{
		CodecID:   optional.Some(media.CodecLPCM),
		ByteOrder: optional.Some[binary.ByteOrder](binary.LittleEndian),
	}
}

func NewDecoderEngine(cfg DecoderConfig) engine.DecoderEngine {
	return internal.NewDecoder(cfg.ApplyDefaults())
}

func NewEncoderEngine(cfg EncoderConfig) engine.EncoderEngine {
	return internal.NewEncoder(cfg.ApplyDefaults())
}

func NewConfigWithAudio(sampleRate int, format media.SampleFormat, layout media.ChannelLayout) DecoderConfig {
	cfg := DefaultDecoderConfig()
	if sampleRate > 0 {
		cfg.SampleRate = optional.Some(sampleRate)
	}
	if format != media.SampleFormatUnknown {
		cfg.Format = optional.Some(format)
	}
	if layout.ChannelCount() > 0 {
		cfg.ChannelLayout = optional.Some(layout)
	}
	return cfg
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
	if err := godec.Register(DecoderConfig{}, registry.DecoderManifest{
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
			c := DefaultDecoderConfig()
			if s.MediaAttributes.Codec != "" {
				c.CodecID = optional.Some(s.MediaAttributes.Codec)
			}
			if s.MediaAttributes.Audio.SampleRate > 0 {
				c.SampleRate = optional.Some(s.MediaAttributes.Audio.SampleRate)
			}
			if s.MediaAttributes.Audio.Format != media.SampleFormatUnknown {
				c.Format = optional.Some(s.MediaAttributes.Audio.Format)
			}
			if s.MediaAttributes.Audio.ChannelLayout.ChannelCount() > 0 {
				c.ChannelLayout = optional.Some(s.MediaAttributes.Audio.ChannelLayout)
			}
			if params, ok, err := adpcmParametersFromStream(s, s.Codec); err != nil {
				return nil, err
			} else if ok {
				c.ADPCM = optional.Some(params)
			}

			if cfg != nil {
				var pcmCfg DecoderConfig
				if pc, ok := cfg.(DecoderConfig); ok {
					pcmCfg = pc
				} else if pcPtr, ok := cfg.(*DecoderConfig); ok && pcPtr != nil {
					pcmCfg = *pcPtr
				}
				// Merge pcmCfg onto c, and output resolved internal config
				resolved := config.ApplyDefaults(pcmCfg, c)
				return engine.WrapDecoder(NewDecoderEngine(resolved)), nil
			}
			return engine.WrapDecoder(NewDecoderEngine(c)), nil
		},
	}); err != nil {
		panic(err)
	}

	// --- Encoder ---
	if err := godec.Register(EncoderConfig{}, registry.EncoderManifest{
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
				resolved, err := resolveEncoderConfig(in, target, cfg)
				if err != nil {
					return media.Profile{}, err
				}
				profile := media.Profile{Type: in.Type, MediaAttributes: in.MediaAttributes}
				profile.Codec = target
				if target == media.CodecMSADPCM || target == media.CodecIMAADPCM {
					channels := profile.Audio.ChannelLayout.ChannelCount()
					adpcm := resolved.ADPCM.Unwrap()
					if err := adpcm.Validate(target, channels); err != nil {
						return media.Profile{}, err
					}
					profile.CodecParameters = media.CodecParameters{Schema: params.SchemaADPCM, Data: adpcm.MarshalBinary()}
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
			encCfg, err := resolveEncoderConfig(inStream, targetCodec, cfg)
			if err != nil {
				return nil, err
			}
			return engine.WrapEncoder(NewEncoderEngine(encCfg)), nil
		},
	}); err != nil {
		panic(err)
	}
}

func adpcmParametersFromStream(stream media.StreamInfo, codec media.CodecID) (params.ADPCM, bool, error) {
	if codec != media.CodecMSADPCM && codec != media.CodecIMAADPCM {
		return params.ADPCM{}, false, nil
	}
	if stream.CodecParameters.Schema != params.SchemaADPCM {
		return params.ADPCM{}, false, nil
	}
	adpcm, err := params.Parse(codec, stream.Audio.ChannelLayout.ChannelCount(), stream.CodecParameters.Data)
	if err != nil {
		return params.ADPCM{}, false, err
	}
	return adpcm, true, nil
}

func resolveEncoderConfig(in media.StreamInfo, target media.CodecID, cfg registry.Configuration) (EncoderConfig, error) {
	base := EncoderConfig{
		CodecID:   optional.Some(target),
		ByteOrder: optional.Some[binary.ByteOrder](binary.LittleEndian),
	}
	if in.Codec == target {
		if adpcm, ok, err := adpcmParametersFromStream(in, target); err != nil {
			return EncoderConfig{}, err
		} else if ok {
			base.ADPCM = optional.Some(adpcm)
		}
	}

	var override EncoderConfig
	if pc, ok := cfg.(EncoderConfig); ok {
		override = pc
	} else if pcPtr, ok := cfg.(*EncoderConfig); ok && pcPtr != nil {
		override = *pcPtr
	}
	resolved := config.ApplyDefaults(override, base)
	if (target == media.CodecMSADPCM || target == media.CodecIMAADPCM) && !resolved.ADPCM.Exists() {
		params, err := params.Default(target, in.Audio.ChannelLayout.ChannelCount())
		if err != nil {
			return EncoderConfig{}, err
		}
		resolved.ADPCM = optional.Some(params)
	}
	return resolved, nil
}
