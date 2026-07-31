package routing

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
)

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
	defer geo.Close()

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

	geo, err := neg.NegotiateConversion(context.Background(), spec)
	if err != nil {
		t.Fatalf("failed to negotiate conversion: %v", err)
	}
	defer geo.Close()

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
