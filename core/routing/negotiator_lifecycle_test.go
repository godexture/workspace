package routing

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
)

func TestNegotiatorResolvesCompletePlanBeforeCreatingTransforms(t *testing.T) {
	t.Parallel()
	var demuxClosed bool
	demux := &mockDemuxer{
		mockNode: mockNode{onClose: func() { demuxClosed = true }},
		streams: []media.StreamInfo{{
			Type: media.MediaAudio,
			MediaAttributes: media.MediaAttributes{
				Codec: media.CodecFLAC,
			},
		}},
	}
	demuxResolver := &mockDemuxerResolver{resolved: registry.DemuxerManifest{
		Factory: func(io.Reader, registry.Configuration) (node.Demuxer, error) {
			return demux, nil
		},
	}}
	var decoderFactoryCalled, decoderProbeClosed bool
	decoderResolver := &mockDecoderResolver{resolved: registry.DecoderManifest{
		Factory: func(stream media.StreamInfo, _ registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
			decoderFactoryCalled = true
			return &mockDecoder{mockNode: mockNode{onClose: func() { decoderProbeClosed = true }}}, stream, nil
		},
	}}
	encoderResolver := &mockEncoderResolver{resolved: registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{})),
		},
		Factory: func(stream media.StreamInfo, target media.CodecID, _ registry.TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
			output := stream.Clone()
			output.Codec = target
			return &mockEncoder{}, output, nil
		},
	}}
	muxResolver := &mockMuxerResolver{err: errors.New("mux resolution")}

	_, err := NewNegotiator(muxResolver, demuxResolver, encoderResolver, decoderResolver, nil, nil).
		NegotiateConversion(context.Background(), ConversionSpec{
			Input:       strings.NewReader("input"),
			Output:      &strings.Builder{},
			TargetCodec: media.CodecFLAC,
			MuxConfig:   dummyConfig{},
		})
	if !errors.Is(err, muxResolver.err) {
		t.Fatalf("NegotiateConversion() error = %v", err)
	}
	if !decoderFactoryCalled {
		t.Fatal("decoder factory was not called to resolve its output profile")
	}
	if !decoderProbeClosed {
		t.Fatal("decoder profile probe was not closed")
	}
	if !demuxClosed {
		t.Fatal("demuxer was not closed after negotiation failed")
	}
}

func TestNegotiatorClosesConstructedNodesWhenFactoryFails(t *testing.T) {
	t.Parallel()
	var closeOrder []string
	demux := &mockDemuxer{
		mockNode: mockNode{onClose: func() { closeOrder = append(closeOrder, "demuxer") }},
		streams: []media.StreamInfo{{
			Type: media.MediaAudio,
			MediaAttributes: media.MediaAttributes{
				Codec: media.CodecFLAC,
			},
		}},
	}
	decoder := &mockDecoder{mockNode: mockNode{
		onClose: func() { closeOrder = append(closeOrder, "decoder") },
	}}
	demuxResolver := &mockDemuxerResolver{resolved: registry.DemuxerManifest{
		Factory: func(io.Reader, registry.Configuration) (node.Demuxer, error) {
			return demux, nil
		},
	}}
	decoderResolver := &mockDecoderResolver{resolved: registry.DecoderManifest{
		Factory: func(stream media.StreamInfo, _ registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
			return decoder, stream, nil
		},
	}}
	factoryErr := errors.New("encoder factory")
	encoderResolver := &mockEncoderResolver{resolved: registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{})),
		},
		Factory: func(media.StreamInfo, media.CodecID, registry.TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
			return nil, media.StreamInfo{}, factoryErr
		},
	}}
	muxResolver := &mockMuxerResolver{resolved: registry.MuxerManifest{
		Codecs: []media.CodecID{media.CodecFLAC},
	}}

	_, err := NewNegotiator(muxResolver, demuxResolver, encoderResolver, decoderResolver, nil, nil).
		NegotiateConversion(context.Background(), ConversionSpec{
			Input:       strings.NewReader("input"),
			Output:      &strings.Builder{},
			TargetCodec: media.CodecFLAC,
			MuxConfig:   dummyConfig{},
		})
	if !errors.Is(err, factoryErr) {
		t.Fatalf("NegotiateConversion() error = %v", err)
	}
	if got, want := closeOrder, []string{"decoder", "demuxer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("close order = %v, want %v", got, want)
	}
}

func TestNegotiatorConstructsEachTransformOnce(t *testing.T) {
	t.Parallel()
	stream := media.StreamInfo{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Codec: media.CodecFLAC}}
	var decoderCalls, encoderCalls int
	geometry, err := NewNegotiator(
		&mockMuxerResolver{resolved: registry.MuxerManifest{
			Codecs:  []media.CodecID{media.CodecFLAC},
			Factory: func(io.Writer, registry.Configuration) (node.Muxer, error) { return &mockMuxer{}, nil },
		}},
		&mockDemuxerResolver{resolved: registry.DemuxerManifest{
			Factory: func(io.Reader, registry.Configuration) (node.Demuxer, error) {
				return &mockDemuxer{streams: []media.StreamInfo{stream}}, nil
			},
		}},
		&mockEncoderResolver{resolved: registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{}))},
			Codecs:            []media.CodecID{media.CodecFLAC},
			Factory: func(input media.StreamInfo, target media.CodecID, _ registry.TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
				encoderCalls++
				output := input.Clone()
				output.Codec = target
				return &mockEncoder{}, output, nil
			},
		}},
		&mockDecoderResolver{resolved: registry.DecoderManifest{
			TransformManifest: registry.TransformManifest{InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{}))},
			Factory: func(input media.StreamInfo, _ registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
				decoderCalls++
				return &mockDecoder{}, input, nil
			},
		}},
		nil,
		nil,
	).NegotiateConversion(context.Background(), ConversionSpec{
		Input:       strings.NewReader("input"),
		Output:      &strings.Builder{},
		TargetCodec: media.CodecFLAC,
		MuxConfig:   dummyConfig{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer geometry.Close()
	if decoderCalls != 1 || encoderCalls != 1 {
		t.Fatalf("factory calls = decoder:%d encoder:%d, want one each", decoderCalls, encoderCalls)
	}
}
