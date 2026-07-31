package routing

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
)

func TestNegotiatorRejectsExplicitIncompatibleDecoder(t *testing.T) {
	demuxer := registry.DemuxerManifest{
		BaseManifest: registry.BaseManifest{Name: "flac"},
		Factory: func(io.Reader, registry.Configuration) (node.Demuxer, error) {
			return &mockDemuxer{
				streams:  []media.StreamInfo{{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Codec: media.CodecFLAC}}},
				metadata: metadata.NewBundle(),
			}, nil
		},
	}
	decoder := registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest: registry.BaseManifest{Name: "pcm"},
			InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(&manifest.AudioConstraint{
				Codecs: []media.CodecID{media.CodecLPCM},
			})),
		},
	}
	negotiator := NewNegotiator(&mockMuxerResolver{}, &mockDemuxerResolver{}, &mockEncoderResolver{}, &mockDecoderResolver{}, nil, nil)
	_, err := negotiator.NegotiateConversion(context.Background(), ConversionSpec{
		Input:           strings.NewReader("input"),
		Output:          &strings.Builder{},
		DemuxManifest:   demuxer,
		DemuxConfig:     dummyConfig{},
		DecoderManifest: decoder,
		DecodeConfig:    dummyConfig{},
		TargetCodec:     media.CodecLPCM,
		MuxConfig:       dummyConfig{},
	})
	if err == nil || !strings.Contains(err.Error(), `decoder "pcm" does not accept input codec "flac"`) {
		t.Fatalf("NegotiateConversion() error = %v", err)
	}
}
