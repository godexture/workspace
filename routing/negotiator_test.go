package routing

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
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

func TestNegotiator_CustomResolvers(t *testing.T) {
	t.Parallel()
	// 1. Set up mock nodes
	streamIn := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Audio: media.AudioAttributes{
				SampleRate: 44100,
			},
		},
	}
	inputMetadata := metadata.NewBundle()
	inputMetadata.Set(metadata.KeyTitle("Input title"))
	demux := &mockDemuxer{streams: []media.StreamInfo{streamIn}, metadata: inputMetadata}
	dec := &mockDecoder{}
	enc := &mockEncoder{}
	mux := &mockMuxer{}

	// 2. Set up mock resolvers returning manifests pointing to mock nodes
	demuxRes := &mockDemuxerResolver{
		resolved: registry.DemuxerManifest{
			Factory: func(r io.Reader, config registry.Configuration) (node.Demuxer, error) {
				return demux, nil
			},
		},
	}
	decRes := &mockDecoderResolver{
		resolved: registry.DecoderManifest{
			Factory: func(stream media.StreamInfo, options registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
				return dec, stream, nil
			},
		},
	}
	encRes := &mockEncoderResolver{
		resolved: registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{
				InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{})),
			},
			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, options registry.TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
				output := inStream.Clone()
				output.Codec = targetCodec
				return enc, output, nil
			},
		},
	}
	muxRes := &mockMuxerResolver{
		resolved: registry.MuxerManifest{
			Codecs: []media.CodecID{media.CodecLPCM},
			Factory: func(w io.Writer, config registry.Configuration) (node.Muxer, error) {
				return mux, nil
			},
		},
	}

	// 3. Create Negotiator with custom resolvers
	neg := NewNegotiator(muxRes, demuxRes, encRes, decRes, nil, nil)

	// 4. Run Negotiation
	spec := ConversionSpec{
		Input:       strings.NewReader("dummy input"),
		Output:      &strings.Builder{},
		TargetCodec: media.CodecLPCM,
		MuxConfig:   dummyConfig{},
	}

	geo, err := neg.NegotiateConversion(context.Background(), spec)
	if err != nil {
		t.Fatalf("failed to negotiate conversion: %v", err)
	}

	// 5. Assertions
	if !demuxRes.called {
		t.Error("custom demuxer resolver was not called")
	}
	if !decRes.called {
		t.Error("custom decoder resolver was not called")
	}
	if !encRes.called {
		t.Error("custom encoder resolver was not called")
	}
	if !muxRes.called {
		t.Error("custom muxer resolver was not called")
	}

	if len(geo.Nodes()) != 4 {
		t.Errorf("expected 4 nodes in geometry, got %d", len(geo.Nodes()))
	}
	if len(geo.Edges()) != 3 {
		t.Errorf("expected 3 edges in geometry, got %d", len(geo.Edges()))
	}

	// Verify muxer received the stream info
	if len(mux.addedStreams) != 1 {
		t.Errorf("expected 1 stream added to muxer, got %d", len(mux.addedStreams))
	} else if mux.addedStreams[0].Codec != media.CodecLPCM {
		t.Errorf("expected target codec %s, got %s", media.CodecLPCM, mux.addedStreams[0].Codec)
	}
	if mux.metadata == inputMetadata {
		t.Fatal("muxer received the demuxer metadata without cloning it")
	}
	metadata.AssertBundleValue(t, mux.metadata, metadata.KeyTitle("Input title"))
}

func TestNegotiator_AppliesTransforms(t *testing.T) {
	t.Parallel()
	// 1. Input stream starts with Unknown format and MSADPCM codec
	streamIn := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecMSADPCM,
			Audio: media.AudioAttributes{
				SampleRate: 44100,
				Format:     media.SampleFormatUnknown,
			},
		},
	}
	demux := &mockDemuxer{streams: []media.StreamInfo{streamIn}}
	dec := &mockDecoder{}
	enc := &mockEncoder{}
	mux := &mockMuxer{}

	demuxRes := &mockDemuxerResolver{
		resolved: registry.DemuxerManifest{
			Factory: func(r io.Reader, config registry.Configuration) (node.Demuxer, error) {
				return demux, nil
			},
		},
	}

	// Decoder Transform converts MSADPCM to LPCM and sets the format to S16
	decRes := &mockDecoderResolver{
		resolved: registry.DecoderManifest{
			Factory: func(stream media.StreamInfo, options registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
				output := stream
				if stream.Codec == media.CodecMSADPCM {
					output.Codec = media.CodecLPCM
					output.Audio.Format = media.SampleFormatS16
				}
				return dec, output, nil
			},
		},
	}

	// Encoder Transform passes it through
	encRes := &mockEncoderResolver{
		resolved: registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{}))},
			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, options registry.TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
				output := inStream.Clone()
				output.Codec = targetCodec
				return enc, output, nil
			},
		},
	}

	muxRes := &mockMuxerResolver{
		resolved: registry.MuxerManifest{
			Codecs: []media.CodecID{media.CodecLPCM},
			Factory: func(w io.Writer, config registry.Configuration) (node.Muxer, error) {
				return mux, nil
			},
		},
	}

	neg := NewNegotiator(muxRes, demuxRes, encRes, decRes, nil, nil)

	spec := ConversionSpec{
		Input:       strings.NewReader("dummy input"),
		Output:      &strings.Builder{},
		TargetCodec: media.CodecLPCM,
		MuxConfig:   dummyConfig{},
	}

	_, err := neg.NegotiateConversion(context.Background(), spec)
	if err != nil {
		t.Fatalf("failed to negotiate conversion: %v", err)
	}

	if len(mux.addedStreams) != 1 {
		t.Fatalf("expected 1 stream added to muxer, got %d", len(mux.addedStreams))
	}

	outStream := mux.addedStreams[0]
	if outStream.Codec != media.CodecLPCM {
		t.Errorf("expected codec %s, got %s", media.CodecLPCM, outStream.Codec)
	}
	if outStream.Audio.Format != media.SampleFormatS16 {
		t.Errorf("expected sample format %s, got %s", media.SampleFormatS16, outStream.Audio.Format)
	}
}

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
