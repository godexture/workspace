package flac

import (
	"github.com/godexture/codec-flac/internal/decoder"
	"github.com/godexture/codec-flac/internal/encoder"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/sdk/engine"
)

func NewDecoderEngine(stream media.StreamInfo, config DecoderConfig) engine.DecoderEngine {
	resolved := engine.ResolveConfig[DecoderConfig](config)
	return decoder.NewDecoder(stream, resolved)
}

func NewEncoderEngine(config EncoderConfig) (engine.EncoderEngine, error) {
	resolved := engine.ResolveConfig[EncoderConfig](config)
	return encoder.NewEncoder(media.StreamInfo{}, resolved)
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
		DecoderConfig{},
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
				resolved := engine.ResolveConfig[DecoderConfig](config)
				return engine.WrapDecoder(decoder.NewDecoder(stream, resolved)), nil
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
				resolved := engine.ResolveConfig[EncoderConfig](cfg)
				enc, err := encoder.NewEncoder(inStream, resolved)
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
