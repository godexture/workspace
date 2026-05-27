package pcm

import (
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
	codecs := []struct {
		id   media.CodecID
		name string
	}{
		{media.CodecLPCM, "pcm"},
		{media.CodecPCMU, "pcmu"},
		{media.CodecPCMA, "pcma"},
	}

	for _, c := range codecs {
		codecID := c.id
		if err := godec.DefaultRegistry.Decoders.Register(Config{}, registry.DecoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        c.name + "-decoder",
					Description: c.name + " decoder",
				},
				Capabilities: []manifest.Capability{pcmCapability{codec: codecID}},
				TransformFunc: func(s media.StreamInfo) media.Profile {
					p := media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
					p.MediaAttributes.Codec = codecID
					p.Audio.CodecID = codecID
					if codecID == media.CodecPCMU || codecID == media.CodecPCMA {
						p.Audio.SampleRate = 8000
						if s.MediaAttributes.Codec == codecID {
							// For decoder: input is G.711, output is LPCM (S16)
							p.Audio.CodecID = media.CodecLPCM
							p.Audio.Format = media.SampleFormatS16
						} else {
							// For encoder: input is LPCM, output is G.711 (U8)
							p.Audio.Format = media.SampleFormatU8
						}
						p.Audio.ChannelLayout = media.LayoutMono1
					}
					return p
				},
			},
			Factory: func(cfg registry.Configuration) (node.Decoder, error) {
				c := DefaultConfig()
				c.CodecID = codecID
				if codecID == media.CodecPCMU || codecID == media.CodecPCMA {
					c.SampleRate = 8000
					c.Format = media.SampleFormatS16
					c.Layout = media.LayoutMono1
				}
				// TODO: load other config from cfg if available
				return engine.WrapDecoder(NewDecoderEngine(c)), nil
			},
		}); err != nil {
			panic(err)
		}

		if err := godec.DefaultRegistry.Encoders.Register(EncoderConfig{}, registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{
				BaseManifest: registry.BaseManifest{
					Name:        c.name + "-encoder",
					Description: c.name + " encoder",
				},
				Capabilities: []manifest.Capability{pcmCapability{codec: codecID}},
				TransformFunc: func(s media.StreamInfo) media.Profile {
					return media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
				},
			},
			Supports: func(codec media.CodecID) bool {
				return codec == codecID
			},
			Factory: func(cfg registry.Configuration) (node.Encoder, error) {
				_ = cfg
				return engine.WrapEncoder(NewEncoderEngine(EncoderConfig{CodecID: codecID})), nil
			},
		}); err != nil {
			panic(err)
		}
	}
}
