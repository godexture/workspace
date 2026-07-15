package routing

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/resolver"
)

type mockNode struct{}

func (m *mockNode) Start(ctx context.Context) error { return nil }

type mockDemuxer struct {
	mockNode
	streams []media.StreamInfo
}

func (m *mockDemuxer) Metadata() *metadata.Bundle                           { return nil }
func (m *mockDemuxer) Streams() ([]media.StreamInfo, error)                 { return m.streams, nil }
func (m *mockDemuxer) OutputPorts() map[string]*node.OutPort[*media.Packet] { return nil }

type mockDecoder struct {
	mockNode
}

func (m *mockDecoder) InputPorts() map[string]*node.InPort[*media.Packet] { return nil }
func (m *mockDecoder) OutputPorts() map[string]*node.OutPort[media.Frame] { return nil }

type mockEncoder struct {
	mockNode
}

func (m *mockEncoder) InputPorts() map[string]*node.InPort[media.Frame]     { return nil }
func (m *mockEncoder) OutputPorts() map[string]*node.OutPort[*media.Packet] { return nil }

type mockMuxer struct {
	mockNode
	addedStreams []media.StreamInfo
}

func (m *mockMuxer) AddStream(info media.StreamInfo) (int, error) {
	m.addedStreams = append(m.addedStreams, info)
	return len(m.addedStreams) - 1, nil
}
func (m *mockMuxer) SetMetadata(meta *metadata.Bundle) error            { return nil }
func (m *mockMuxer) InputPorts() map[string]*node.InPort[*media.Packet] { return nil }

// Mock Resolvers
type mockDemuxerResolver struct {
	resolved registry.DemuxerManifest
	called   bool
}

func (r *mockDemuxerResolver) ResolveDemuxer(stream io.ReadSeeker, opts ...resolver.Option) (registry.DemuxerManifest, error) {
	r.called = true
	return r.resolved, nil
}

type mockDecoderResolver struct {
	resolved registry.DecoderManifest
	called   bool
}

func (r *mockDecoderResolver) ResolveDecoder(stream media.StreamInfo, opts ...resolver.Option) (registry.DecoderManifest, error) {
	r.called = true
	return r.resolved, nil
}

type mockEncoderResolver struct {
	resolved registry.EncoderManifest
	called   bool
}

func (r *mockEncoderResolver) ResolveEncoder(codec media.CodecID, opts ...resolver.Option) (registry.EncoderManifest, error) {
	r.called = true
	return r.resolved, nil
}

type mockMuxerResolver struct {
	resolved registry.MuxerManifest
	called   bool
}

func (r *mockMuxerResolver) ResolveMuxer(config registry.Configuration) (registry.MuxerManifest, error) {
	r.called = true
	return r.resolved, nil
}

type dummyConfig struct{}

func (dummyConfig) NodeConfiguration() {}

func TestNegotiator_CustomResolvers(t *testing.T) {
	// 1. Set up mock nodes
	streamIn := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Audio: media.AudioAttributes{
				SampleRate: 44100,
			},
		},
	}
	demux := &mockDemuxer{streams: []media.StreamInfo{streamIn}}
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
			Factory: func(stream media.StreamInfo, config registry.Configuration) (node.Decoder, error) {
				return dec, nil
			},
		},
	}
	encRes := &mockEncoderResolver{
		resolved: registry.EncoderManifest{
			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, config registry.Configuration) (node.Encoder, error) {
				return enc, nil
			},
		},
	}
	muxRes := &mockMuxerResolver{
		resolved: registry.MuxerManifest{
			Factory: func(w io.Writer, config registry.Configuration) (node.Muxer, error) {
				return mux, nil
			},
		},
	}

	// 3. Create Negotiator with custom resolvers
	neg := NewNegotiator(muxRes, demuxRes, encRes, decRes)

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

	if len(geo.Nodes) != 4 {
		t.Errorf("expected 4 nodes in geometry, got %d", len(geo.Nodes))
	}
	if len(geo.Edges) != 3 {
		t.Errorf("expected 3 edges in geometry, got %d", len(geo.Edges))
	}

	// Verify muxer received the stream info
	if len(mux.addedStreams) != 1 {
		t.Errorf("expected 1 stream added to muxer, got %d", len(mux.addedStreams))
	} else if mux.addedStreams[0].Codec != media.CodecLPCM {
		t.Errorf("expected target codec %s, got %s", media.CodecLPCM, mux.addedStreams[0].Codec)
	}
}

func TestNegotiator_AppliesTransforms(t *testing.T) {
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
			TransformManifest: registry.TransformManifest{
				TransformFunc: func(s media.StreamInfo, _ media.CodecID, _ registry.Configuration) (media.Profile, error) {
					p := media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
					if s.Codec == media.CodecMSADPCM {
						p.Codec = media.CodecLPCM
						p.Audio.Format = media.SampleFormatS16
					}
					return p, nil
				},
			},
			Factory: func(stream media.StreamInfo, config registry.Configuration) (node.Decoder, error) {
				return dec, nil
			},
		},
	}

	// Encoder Transform passes it through
	encRes := &mockEncoderResolver{
		resolved: registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{
				TransformFunc: func(s media.StreamInfo, target media.CodecID, _ registry.Configuration) (media.Profile, error) {
					p := media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
					p.Codec = target
					return p, nil
				},
			},
			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, config registry.Configuration) (node.Encoder, error) {
				return enc, nil
			},
		},
	}

	muxRes := &mockMuxerResolver{
		resolved: registry.MuxerManifest{
			Factory: func(w io.Writer, config registry.Configuration) (node.Muxer, error) {
				return mux, nil
			},
		},
	}

	neg := NewNegotiator(muxRes, demuxRes, encRes, decRes)

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
