package pcm

import (
	"encoding/binary"

	internal "github.com/godexture/codec-pcm/internal"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/engine"
)

type Config = internal.Config
type EncoderConfig = internal.EncoderConfig

func DefaultConfig() Config {
	return internal.DefaultConfig()
}

func NewDecoderEngine(config Config) engine.DecoderEngine {
	return internal.NewDecoder(config)
}

func NewEncoderEngine(config EncoderConfig) engine.EncoderEngine {
	return internal.NewEncoder(config)
}

func NewConfigWithAudio(sampleRate int, format media.SampleFormat, layout media.ChannelLayout) Config {
	cfg := internal.DefaultConfig()
	if sampleRate > 0 {
		cfg.SampleRate = sampleRate
	}
	if format != media.SampleFormatUnknown {
		cfg.Format = format
	}
	if layout.ChannelCount() > 0 {
		cfg.Layout = layout
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
	if err := godec.Register(Config{}, registry.DecoderManifest{
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
			TransformFunc: func(s media.StreamInfo) media.Profile {
				p := media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
				codecID := s.MediaAttributes.Codec
				p.Codec = media.CodecLPCM
				if codecID == media.CodecPCMU || codecID == media.CodecPCMA {
					p.Audio.SampleRate = 8000
					p.Audio.Format = media.SampleFormatS16
					p.Audio.ChannelLayout = media.LayoutMono1
				} else if codecID == media.CodecMSADPCM || codecID == media.CodecIMAADPCM {
					p.Audio.Format = media.SampleFormatS16
				}
				return p
			},
		},
		Factory: func(cfg registry.Configuration) (node.Decoder, error) {
			c := DefaultConfig()
			if cfg != nil {
				if pcmCfg, ok := cfg.(Config); ok {
					if pcmCfg.CodecID != "" {
						c.CodecID = pcmCfg.CodecID
					}
					if pcmCfg.SampleRate > 0 {
						c.SampleRate = pcmCfg.SampleRate
					}
					if pcmCfg.Format != media.SampleFormatUnknown {
						c.Format = pcmCfg.Format
					}
					if pcmCfg.Layout.ChannelCount() > 0 {
						c.Layout = pcmCfg.Layout
					}
					if pcmCfg.ByteOrder != nil {
						c.ByteOrder = pcmCfg.ByteOrder
					}
				} else if pcmCfgPtr, ok := cfg.(*Config); ok && pcmCfgPtr != nil {
					if pcmCfgPtr.CodecID != "" {
						c.CodecID = pcmCfgPtr.CodecID
					}
					if pcmCfgPtr.SampleRate > 0 {
						c.SampleRate = pcmCfgPtr.SampleRate
					}
					if pcmCfgPtr.Format != media.SampleFormatUnknown {
						c.Format = pcmCfgPtr.Format
					}
					if pcmCfgPtr.Layout.ChannelCount() > 0 {
						c.Layout = pcmCfgPtr.Layout
					}
					if pcmCfgPtr.ByteOrder != nil {
						c.ByteOrder = pcmCfgPtr.ByteOrder
					}
				}
			}
			// Set G.711 default sample rate & layout if not explicitly set
			if c.CodecID == media.CodecPCMU || c.CodecID == media.CodecPCMA {
				if c.SampleRate == 48000 {
					c.SampleRate = 8000
				}
				if c.Layout == media.LayoutStereo2_0 {
					c.Layout = media.LayoutMono1
				}
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
			TransformFunc: func(s media.StreamInfo) media.Profile {
				return media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
			},
		},
		Supports: func(codec media.CodecID) bool {
			return codec == media.CodecLPCM || codec == media.CodecPCMU || codec == media.CodecPCMA || codec == media.CodecMSADPCM || codec == media.CodecIMAADPCM
		},
		Factory: func(cfg registry.Configuration) (node.Encoder, error) {
			encCfg := EncoderConfig{CodecID: media.CodecLPCM, ByteOrder: binary.LittleEndian}
			if cfg != nil {
				if pcmEncCfg, ok := cfg.(EncoderConfig); ok {
					encCfg.CodecID = pcmEncCfg.CodecID
					if pcmEncCfg.ByteOrder != nil {
						encCfg.ByteOrder = pcmEncCfg.ByteOrder
					}
				} else if pcmEncCfgPtr, ok := cfg.(*EncoderConfig); ok && pcmEncCfgPtr != nil {
					encCfg.CodecID = pcmEncCfgPtr.CodecID
					if pcmEncCfgPtr.ByteOrder != nil {
						encCfg.ByteOrder = pcmEncCfgPtr.ByteOrder
					}
				}
			}
			return engine.WrapEncoder(NewEncoderEngine(encCfg)), nil
		},
	}); err != nil {
		panic(err)
	}
}
