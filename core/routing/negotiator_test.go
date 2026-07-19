package routing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/resolver"
)

type mockNode struct {
	onClose func()
}

func (m *mockNode) Start(ctx context.Context) error { return nil }
func (m *mockNode) Close() error {
	if m.onClose != nil {
		m.onClose()
	}
	return nil
}

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

type mockFilter struct {
	mockNode
}

func (m *mockFilter) Process(context.Context) error                      { return nil }
func (m *mockFilter) InputPorts() map[string]*node.InPort[media.Frame]   { return nil }
func (m *mockFilter) OutputPorts() map[string]*node.OutPort[media.Frame] { return nil }

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
	err      error
}

func (r *mockMuxerResolver) ResolveMuxer(config registry.Configuration) (registry.MuxerManifest, error) {
	r.called = true
	if r.err != nil {
		return registry.MuxerManifest{}, r.err
	}
	return r.resolved, nil
}

type mockFilterResolver struct {
	resolved []registry.FilterManifest
	configs  []registry.Configuration
}

func (r *mockFilterResolver) ResolveFilter(config registry.Configuration) (registry.FilterManifest, error) {
	r.configs = append(r.configs, config)
	index := len(r.configs) - 1
	if index >= len(r.resolved) {
		return registry.FilterManifest{}, fmt.Errorf("unexpected filter %d", index)
	}
	return r.resolved[index], nil
}

type dummyConfig struct{}
type firstFilterConfig struct{}
type secondFilterConfig struct{}

type alwaysCapability struct{}

func (alwaysCapability) MediaType() media.MediaType     { return media.MediaAudio }
func (alwaysCapability) Match(media.StreamInfo) bool    { return true }
func (alwaysCapability) Diagnose(media.StreamInfo) bool { return true }

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
			Factory: func(stream media.StreamInfo, options registry.TransformFactoryOptions) (node.Decoder, error) {
				return dec, nil
			},
		},
	}
	encRes := &mockEncoderResolver{
		resolved: registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{
				Capabilities: []manifest.Capability{alwaysCapability{}},
			},
			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, options registry.TransformFactoryOptions) (node.Encoder, error) {
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
	neg := NewNegotiator(muxRes, demuxRes, encRes, decRes, nil)

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
			Factory: func(stream media.StreamInfo, options registry.TransformFactoryOptions) (node.Decoder, error) {
				return dec, nil
			},
		},
	}

	// Encoder Transform passes it through
	encRes := &mockEncoderResolver{
		resolved: registry.EncoderManifest{
			TransformManifest: registry.TransformManifest{
				Capabilities: []manifest.Capability{alwaysCapability{}},
				TransformFunc: func(s media.StreamInfo, target media.CodecID, _ registry.Configuration) (media.Profile, error) {
					p := media.Profile{Type: s.Type, MediaAttributes: s.MediaAttributes}
					p.Codec = target
					return p, nil
				},
			},
			Factory: func(inStream media.StreamInfo, targetCodec media.CodecID, options registry.TransformFactoryOptions) (node.Encoder, error) {
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

	neg := NewNegotiator(muxRes, demuxRes, encRes, decRes, nil)

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

func TestNegotiator_AllocatesResourcesAcrossOrderedFilters(t *testing.T) {
	t.Parallel()
	streamIn := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecFLAC,
			Audio: media.AudioAttributes{SampleRate: 44100},
		},
	}
	demux := &mockDemuxer{streams: []media.StreamInfo{streamIn}}
	mux := &mockMuxer{}
	demuxRes := &mockDemuxerResolver{resolved: registry.DemuxerManifest{
		Factory: func(io.Reader, registry.Configuration) (node.Demuxer, error) {
			return demux, nil
		},
	}}
	muxRes := &mockMuxerResolver{resolved: registry.MuxerManifest{
		Factory: func(io.Writer, registry.Configuration) (node.Muxer, error) {
			return mux, nil
		},
	}}

	var allocations []int
	var inputs []media.StreamInfo
	decoderRes := &mockDecoderResolver{resolved: registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{
			Resources: registry.ResourceRequest{Parallelism: true},
			TransformFunc: func(stream media.StreamInfo, _ media.CodecID, _ registry.Configuration) (media.Profile, error) {
				profile := media.Profile{Type: stream.Type, MediaAttributes: stream.MediaAttributes}
				profile.Codec = media.CodecLPCM
				return profile, nil
			},
		},
		Factory: func(input media.StreamInfo, options registry.TransformFactoryOptions) (node.Decoder, error) {
			inputs = append(inputs, input)
			allocations = append(allocations, options.Resources.Parallelism)
			return &mockDecoder{}, nil
		},
	}}

	filterManifest := func(parallel bool, sampleRateDelta int) registry.FilterManifest {
		return registry.FilterManifest{
			TransformManifest: registry.TransformManifest{
				Capabilities: []manifest.Capability{alwaysCapability{}},
				Resources:    registry.ResourceRequest{Parallelism: parallel},
				TransformFunc: func(stream media.StreamInfo, _ media.CodecID, _ registry.Configuration) (media.Profile, error) {
					profile := media.Profile{Type: stream.Type, MediaAttributes: stream.MediaAttributes}
					profile.Audio.SampleRate += sampleRateDelta
					return profile, nil
				},
			},
			Factory: func(input media.StreamInfo, options registry.TransformFactoryOptions) (node.Filter, error) {
				inputs = append(inputs, input)
				allocations = append(allocations, options.Resources.Parallelism)
				return &mockFilter{}, nil
			},
		}
	}
	filterRes := &mockFilterResolver{resolved: []registry.FilterManifest{
		filterManifest(true, 1),
		filterManifest(false, 2),
	}}
	encoderRes := &mockEncoderResolver{resolved: registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			Capabilities: []manifest.Capability{alwaysCapability{}},
			Resources:    registry.ResourceRequest{Parallelism: true},
			TransformFunc: func(stream media.StreamInfo, target media.CodecID, _ registry.Configuration) (media.Profile, error) {
				profile := media.Profile{Type: stream.Type, MediaAttributes: stream.MediaAttributes}
				profile.Codec = target
				return profile, nil
			},
		},
		Factory: func(input media.StreamInfo, _ media.CodecID, options registry.TransformFactoryOptions) (node.Encoder, error) {
			inputs = append(inputs, input)
			allocations = append(allocations, options.Resources.Parallelism)
			return &mockEncoder{}, nil
		},
	}}

	negotiator := NewNegotiator(muxRes, demuxRes, encoderRes, decoderRes, filterRes)
	geometry, err := negotiator.NegotiateConversion(context.Background(), ConversionSpec{
		Input:       strings.NewReader("input"),
		Output:      &strings.Builder{},
		Filters:     []FilterSpec{{Config: firstFilterConfig{}}, {Config: secondFilterConfig{}}},
		TargetCodec: media.CodecFLAC,
		MuxConfig:   dummyConfig{},
		Resources:   registry.ResourceBudget{Parallelism: 8},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := allocations, []int{3, 3, 0, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allocations = %v, want %v", got, want)
	}
	if got, want := []int{
		inputs[0].Audio.SampleRate,
		inputs[1].Audio.SampleRate,
		inputs[2].Audio.SampleRate,
		inputs[3].Audio.SampleRate,
	}, []int{44100, 44100, 44101, 44103}; !reflect.DeepEqual(got, want) {
		t.Fatalf("factory input sample rates = %v, want %v", got, want)
	}
	nodes := geometry.Nodes()
	edges := geometry.Edges()
	if len(nodes) != 6 || len(edges) != 5 {
		t.Fatalf("geometry has %d nodes and %d edges, want 6 and 5", len(nodes), len(edges))
	}
	if nodes[2].ID != "filter:0" || nodes[3].ID != "filter:1" {
		t.Fatalf("filters are not ordered in geometry: %#v", nodes)
	}
	if got := mux.addedStreams[0].Audio.SampleRate; got != 44103 {
		t.Fatalf("muxer sample rate = %d, want 44103", got)
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
	var decoderFactoryCalled bool
	decoderResolver := &mockDecoderResolver{resolved: registry.DecoderManifest{
		Factory: func(media.StreamInfo, registry.TransformFactoryOptions) (node.Decoder, error) {
			decoderFactoryCalled = true
			return &mockDecoder{}, nil
		},
	}}
	encoderResolver := &mockEncoderResolver{resolved: registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			Capabilities: []manifest.Capability{alwaysCapability{}},
		},
	}}
	muxResolver := &mockMuxerResolver{err: errors.New("mux resolution")}

	_, err := NewNegotiator(muxResolver, demuxResolver, encoderResolver, decoderResolver, nil).
		NegotiateConversion(context.Background(), ConversionSpec{
			Input:       strings.NewReader("input"),
			Output:      &strings.Builder{},
			TargetCodec: media.CodecFLAC,
			MuxConfig:   dummyConfig{},
		})
	if !errors.Is(err, muxResolver.err) {
		t.Fatalf("NegotiateConversion() error = %v", err)
	}
	if decoderFactoryCalled {
		t.Fatal("decoder was constructed before the full plan resolved")
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
		Factory: func(media.StreamInfo, registry.TransformFactoryOptions) (node.Decoder, error) {
			return decoder, nil
		},
	}}
	factoryErr := errors.New("encoder factory")
	encoderResolver := &mockEncoderResolver{resolved: registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			Capabilities: []manifest.Capability{alwaysCapability{}},
		},
		Factory: func(media.StreamInfo, media.CodecID, registry.TransformFactoryOptions) (node.Encoder, error) {
			return nil, factoryErr
		},
	}}
	muxResolver := &mockMuxerResolver{resolved: registry.MuxerManifest{}}

	_, err := NewNegotiator(muxResolver, demuxResolver, encoderResolver, decoderResolver, nil).
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
