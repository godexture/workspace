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
	streams  []media.StreamInfo
	metadata *metadata.Bundle
}

func (m *mockDemuxer) Metadata() *metadata.Bundle                           { return m.metadata }
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
	inputs map[string]*node.InPort[media.Frame]
}

func (m *mockFilter) Process(context.Context) error                      { return nil }
func (m *mockFilter) InputPorts() map[string]*node.InPort[media.Frame]   { return m.inputs }
func (m *mockFilter) OutputPorts() map[string]*node.OutPort[media.Frame] { return nil }

type mockMuxer struct {
	mockNode
	addedStreams []media.StreamInfo
	metadata     *metadata.Bundle
}

func (m *mockMuxer) AddStream(info media.StreamInfo) (int, error) {
	m.addedStreams = append(m.addedStreams, info)
	return len(m.addedStreams) - 1, nil
}
func (m *mockMuxer) SetMetadata(meta *metadata.Bundle) error            { m.metadata = meta; return nil }
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

type mockBridgeResolver struct {
	steps  []resolver.BridgeStep
	called bool
}

func (r *mockBridgeResolver) ResolveBridge(media.StreamInfo, []manifest.Capability) ([]resolver.BridgeStep, error) {
	r.called = true
	return r.steps, nil
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
type auxFilterConfig struct{}

type alwaysCapability struct{}

func (alwaysCapability) Match(media.StreamInfo) bool     { return true }
func (alwaysCapability) Diagnose(media.StreamInfo) error { return nil }

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

func TestNegotiatorConnectsNamedAuxiliaryInput(t *testing.T) {
	t.Parallel()
	mainStream := media.StreamInfo{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Codec: media.CodecFLAC}}
	auxStream := media.StreamInfo{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Codec: media.CodecFLAC}}
	mainDemux := &mockDemuxer{streams: []media.StreamInfo{mainStream}}
	auxDemux := &mockDemuxer{streams: []media.StreamInfo{auxStream}}
	decoderManifest := registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{}))},
		Factory: func(input media.StreamInfo, _ registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
			output := input
			output.Codec = media.CodecLPCM
			return &mockDecoder{}, output, nil
		},
	}
	filterManifest := registry.FilterManifest{
		TransformManifest: registry.TransformManifest{InputRequirements: registry.InputRequirements{
			"in": registry.StaticRequirements(alwaysCapability{}),
			"ir": registry.StaticRequirements(&manifest.AudioConstraint{Codecs: []media.CodecID{media.CodecLPCM}}),
		}},
		Factory: func(inputs media.StreamSet, _ registry.TransformFactoryOptions) (node.Filter, media.StreamSet, error) {
			return &mockFilter{inputs: map[string]*node.InPort[media.Frame]{"in": nil, "ir": nil}}, media.StreamSet{"out": inputs["in"]}, nil
		},
	}
	encoderManifest := registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{}))},
		Codecs:            []media.CodecID{media.CodecFLAC},
		Factory: func(input media.StreamInfo, target media.CodecID, _ registry.TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
			output := input
			output.Codec = target
			return &mockEncoder{}, output, nil
		},
	}
	demuxResolver := &mockDemuxerResolver{resolved: registry.DemuxerManifest{Factory: func(io.Reader, registry.Configuration) (node.Demuxer, error) { return mainDemux, nil }}}
	decoderResolver := &mockDecoderResolver{resolved: decoderManifest}
	filterResolver := &mockFilterResolver{resolved: []registry.FilterManifest{filterManifest, filterManifest}}
	encoderResolver := &mockEncoderResolver{resolved: encoderManifest}
	muxResolver := &mockMuxerResolver{resolved: registry.MuxerManifest{Codecs: []media.CodecID{media.CodecFLAC}, Factory: func(io.Writer, registry.Configuration) (node.Muxer, error) { return &mockMuxer{}, nil }}}
	auxDemuxManifest := registry.DemuxerManifest{Factory: func(io.Reader, registry.Configuration) (node.Demuxer, error) { return auxDemux, nil }}

	geometry, err := NewNegotiator(muxResolver, demuxResolver, encoderResolver, decoderResolver, filterResolver, nil).NegotiateConversion(context.Background(), ConversionSpec{
		Input:       strings.NewReader("main"),
		Output:      &strings.Builder{},
		Filters:     []FilterSpec{{Config: auxFilterConfig{}, Inputs: map[string]PortRef{"ir": {Alias: "IR"}}}},
		TargetCodec: media.CodecFLAC,
		MuxConfig:   dummyConfig{},
		AuxInputs: map[string]AuxInputSpec{
			"IR": {Source: strings.NewReader("aux"), DemuxManifest: auxDemuxManifest},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer geometry.Close()
	var found bool
	for _, edge := range geometry.Edges() {
		if edge.FromNode == "aux:IR:decoder" && edge.ToNode == "filter:0" && edge.ToPort == "ir" {
			found = true
		}
	}
	if !found {
		t.Fatalf("auxiliary edge not found: %#v", geometry.Edges())
	}
	nodes := geometry.Nodes()
	if nodes[2].ID != "aux:IR:decoder" || nodes[3].ID != "decoder" {
		t.Fatalf("auxiliary transform was not inserted before main transform: %#v", nodes)
	}
}

func TestNegotiatorBridgesNamedAuxiliaryInput(t *testing.T) {
	t.Parallel()
	stream := media.StreamInfo{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Codec: media.CodecFLAC}}
	bridgeOutput := stream.Clone()
	bridgeOutput.Codec = media.CodecLPCM
	decoderManifest := registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{}))},
		Factory: func(input media.StreamInfo, _ registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
			return &mockDecoder{}, input, nil
		},
	}
	filterManifest := registry.FilterManifest{
		TransformManifest: registry.TransformManifest{InputRequirements: registry.InputRequirements{
			"in": registry.StaticRequirements(alwaysCapability{}),
			"ir": registry.StaticRequirements(&manifest.AudioConstraint{Codecs: []media.CodecID{media.CodecLPCM}}),
		}},
		Factory: func(inputs media.StreamSet, _ registry.TransformFactoryOptions) (node.Filter, media.StreamSet, error) {
			return &mockFilter{inputs: map[string]*node.InPort[media.Frame]{"in": nil, "ir": nil}}, media.StreamSet{"out": inputs["in"]}, nil
		},
	}
	bridgeManifest := registry.FilterManifest{
		TransformManifest: registry.TransformManifest{InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{}))},
		Factory: registry.SingleFactory(func(media.StreamInfo, registry.TransformFactoryOptions) (node.Filter, media.StreamInfo, error) {
			return &mockFilter{}, bridgeOutput, nil
		}),
	}
	encoderManifest := registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{}))},
		Codecs:            []media.CodecID{media.CodecFLAC},
		Factory: func(input media.StreamInfo, target media.CodecID, _ registry.TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
			output := input.Clone()
			output.Codec = target
			return &mockEncoder{}, output, nil
		},
	}
	mainDemux := &mockDemuxer{streams: []media.StreamInfo{stream}}
	auxDemux := &mockDemuxer{streams: []media.StreamInfo{stream}}
	demuxResolver := &mockDemuxerResolver{resolved: registry.DemuxerManifest{Factory: func(io.Reader, registry.Configuration) (node.Demuxer, error) { return mainDemux, nil }}}
	auxDemuxManifest := registry.DemuxerManifest{Factory: func(io.Reader, registry.Configuration) (node.Demuxer, error) { return auxDemux, nil }}
	filterResolver := &mockFilterResolver{resolved: []registry.FilterManifest{filterManifest}}
	bridgeResolver := &mockBridgeResolver{steps: []resolver.BridgeStep{{
		Input: stream, Output: bridgeOutput, Manifest: bridgeManifest, Config: dummyConfig{},
	}}}
	muxResolver := &mockMuxerResolver{resolved: registry.MuxerManifest{Codecs: []media.CodecID{media.CodecFLAC}, Factory: func(io.Writer, registry.Configuration) (node.Muxer, error) { return &mockMuxer{}, nil }}}

	geometry, err := NewNegotiator(
		muxResolver,
		demuxResolver,
		&mockEncoderResolver{resolved: encoderManifest},
		&mockDecoderResolver{resolved: decoderManifest},
		filterResolver,
		bridgeResolver,
	).NegotiateConversion(context.Background(), ConversionSpec{
		Input:       strings.NewReader("main"),
		Output:      &strings.Builder{},
		Filters:     []FilterSpec{{Config: auxFilterConfig{}, Inputs: map[string]PortRef{"ir": {Alias: "IR"}}}},
		TargetCodec: media.CodecFLAC,
		MuxConfig:   dummyConfig{},
		AuxInputs: map[string]AuxInputSpec{
			"IR": {Source: strings.NewReader("aux"), DemuxManifest: auxDemuxManifest},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer geometry.Close()
	if !bridgeResolver.called {
		t.Fatal("auxiliary incompatibility did not use the bridge resolver")
	}
	for _, edge := range geometry.Edges() {
		if edge.FromNode == "bridge:0" && edge.ToNode == "filter:0" && edge.ToPort == "ir" && edge.Stream.Codec == media.CodecLPCM {
			return
		}
	}
	t.Fatalf("bridged auxiliary edge not found: %#v", geometry.Edges())
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

func TestNegotiatorInsertsBridgeFilters(t *testing.T) {
	t.Parallel()
	streamIn := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecFLAC,
			Audio: media.AudioAttributes{
				SampleRate:    44100,
				Format:        media.SampleFormatS16,
				BitsPerSample: 16,
				ChannelLayout: media.LayoutStereo2_0,
			},
		},
	}
	demux := &mockDemuxer{streams: []media.StreamInfo{streamIn}}
	mux := &mockMuxer{}
	decoder := &mockDecoder{}
	bridgeFilter := &mockFilter{}
	encoder := &mockEncoder{}

	demuxResolver := &mockDemuxerResolver{resolved: registry.DemuxerManifest{
		Factory: func(io.Reader, registry.Configuration) (node.Demuxer, error) { return demux, nil },
	}}
	decoderResolver := &mockDecoderResolver{resolved: registry.DecoderManifest{
		Factory: func(stream media.StreamInfo, _ registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
			output := stream
			output.Codec = media.CodecLPCM
			return decoder, output, nil
		},
	}}
	var bridgeInput media.StreamInfo
	bridgeManifest := registry.FilterManifest{
		TransformManifest: registry.TransformManifest{
			BaseManifest:      registry.BaseManifest{Name: "bridge-format"},
			InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{})),
		},
		Factory: registry.SingleFactory(func(input media.StreamInfo, _ registry.TransformFactoryOptions) (node.Filter, media.StreamInfo, error) {
			bridgeInput = input
			output := input.Clone()
			output.Audio.Format = media.SampleFormatF32
			output.Audio.BitsPerSample = 32
			return bridgeFilter, output, nil
		}),
	}
	decoderOutput := streamIn
	decoderOutput.Codec = media.CodecLPCM
	bridgeProbe, bridgeOutputs, err := bridgeManifest.Factory(media.StreamSet{"in": decoderOutput}, registry.TransformFactoryOptions{Config: struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	bridgeOutput := bridgeOutputs["out"]
	if err := bridgeProbe.Close(); err != nil {
		t.Fatal(err)
	}
	bridgeResolver := &mockBridgeResolver{steps: []resolver.BridgeStep{{
		Manifest: bridgeManifest,
		Config:   struct{}{},
		Input:    decoderOutput,
		Output:   bridgeOutput,
	}}}
	var encoderInput media.StreamInfo
	encoderResolver := &mockEncoderResolver{resolved: registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(&manifest.AudioConstraint{
				Codecs: []media.CodecID{media.CodecLPCM},
				SampleFormats: []manifest.SampleFormatConstraint{{
					Format: media.SampleFormatF32,
				}},
			})),
		},
		Factory: func(input media.StreamInfo, target media.CodecID, _ registry.TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
			encoderInput = input
			output := input.Clone()
			output.Codec = target
			return encoder, output, nil
		},
	}}
	muxResolver := &mockMuxerResolver{resolved: registry.MuxerManifest{
		Codecs:  []media.CodecID{media.CodecFLAC},
		Factory: func(io.Writer, registry.Configuration) (node.Muxer, error) { return mux, nil },
	}}

	geometry, err := NewNegotiator(muxResolver, demuxResolver, encoderResolver, decoderResolver, nil, bridgeResolver).
		NegotiateConversion(context.Background(), ConversionSpec{
			Input:       strings.NewReader("input"),
			Output:      &strings.Builder{},
			TargetCodec: media.CodecFLAC,
			MuxConfig:   dummyConfig{},
		})
	if err != nil {
		t.Fatal(err)
	}
	if !bridgeResolver.called {
		t.Fatal("bridge resolver was not called")
	}
	if got, want := bridgeInput.Audio.Format, media.SampleFormatS16; got != want {
		t.Fatalf("bridge input format = %s, want %s", got, want)
	}
	if got, want := encoderInput.Audio.Format, media.SampleFormatF32; got != want {
		t.Fatalf("encoder input format = %s, want %s", got, want)
	}
	if got, want := len(geometry.Nodes()), 5; got != want {
		t.Fatalf("geometry nodes = %d, want %d", got, want)
	}
	description := geometry.Description()
	for _, node := range description.Nodes {
		want := node.ID == "bridge:0"
		if node.AutoInserted != want {
			t.Errorf("node %q AutoInserted = %t, want %t", node.ID, node.AutoInserted, want)
		}
	}
}

func TestNegotiatorRejectsDiscontinuousBridgePlan(t *testing.T) {
	t.Parallel()
	current := media.StreamInfo{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{Audio: media.AudioAttributes{SampleRate: 44100}}}
	output := current
	output.Audio.SampleRate = 48000
	bridge := &mockBridgeResolver{steps: []resolver.BridgeStep{{
		Input:  media.StreamInfo{Type: media.MediaAudio},
		Output: output,
	}}}
	_, _, err := (&Negotiator{bridgeResolver: bridge}).satisfy(current, []manifest.Capability{&manifest.AudioConstraint{SampleRates: manifest.IntConstraint{Values: []int{48000}}}}, new(int))
	if err == nil || !strings.Contains(err.Error(), "input does not match") {
		t.Fatalf("satisfy() error = %v, want continuity error", err)
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
		Codecs: []media.CodecID{media.CodecFLAC},
		Factory: func(io.Writer, registry.Configuration) (node.Muxer, error) {
			return mux, nil
		},
	}}

	decoderRes := &mockDecoderResolver{resolved: registry.DecoderManifest{
		TransformManifest: registry.TransformManifest{
			Resources: registry.ResourceRequest{Parallelism: true},
		},
		Factory: func(input media.StreamInfo, _ registry.TransformFactoryOptions) (node.Decoder, media.StreamInfo, error) {
			output := input.Clone()
			output.Codec = media.CodecLPCM
			return &mockDecoder{}, output, nil
		},
	}}

	filterManifest := func(parallel bool, sampleRateDelta int) registry.FilterManifest {
		return registry.FilterManifest{
			TransformManifest: registry.TransformManifest{
				InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{})),
				Resources:         registry.ResourceRequest{Parallelism: parallel},
			},
			Factory: registry.SingleFactory(func(input media.StreamInfo, _ registry.TransformFactoryOptions) (node.Filter, media.StreamInfo, error) {
				output := input.Clone()
				output.Audio.SampleRate += sampleRateDelta
				return &mockFilter{}, output, nil
			}),
		}
	}
	filterRes := &mockFilterResolver{resolved: []registry.FilterManifest{
		filterManifest(true, 1),
		filterManifest(false, 2),
	}}
	encoderRes := &mockEncoderResolver{resolved: registry.EncoderManifest{
		TransformManifest: registry.TransformManifest{
			InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(alwaysCapability{})),
			Resources:         registry.ResourceRequest{Parallelism: true},
		},
		Factory: func(input media.StreamInfo, target media.CodecID, _ registry.TransformFactoryOptions) (node.Encoder, media.StreamInfo, error) {
			output := input.Clone()
			output.Codec = target
			return &mockEncoder{}, output, nil
		},
	}}

	negotiator := NewNegotiator(muxRes, demuxRes, encoderRes, decoderRes, filterRes, nil)
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

	nodes := geometry.Nodes()
	edges := geometry.Edges()
	if len(nodes) != 6 || len(edges) != 5 {
		t.Fatalf("geometry has %d nodes and %d edges, want 6 and 5", len(nodes), len(edges))
	}
	if nodes[2].ID != "filter:0" || nodes[3].ID != "filter:1" {
		t.Fatalf("filters are not ordered in geometry: %#v", nodes)
	}
	if nodes[1].Description.Resources.Pool == nil || nodes[2].Description.Resources.Pool == nil || nodes[4].Description.Resources.Pool == nil {
		t.Fatal("parallel-eligible stages got no pool")
	}
	if nodes[1].Description.Resources.Pool != nodes[2].Description.Resources.Pool || nodes[1].Description.Resources.Pool != nodes[4].Description.Resources.Pool {
		t.Fatal("parallel-eligible stages did not share the same pool")
	}
	if nodes[3].Description.Resources.Pool != nil {
		t.Fatal("non-parallel filter got a pool")
	}
	if got := mux.addedStreams[0].Audio.SampleRate; got != 44103 {
		t.Fatalf("muxer sample rate = %d, want 44103", got)
	}
	description := geometry.Description()
	wantRoles := []manifest.NodeType{
		manifest.RoleDemuxer,
		manifest.RoleDecoder,
		manifest.RoleFilter,
		manifest.RoleFilter,
		manifest.RoleEncoder,
		manifest.RoleMuxer,
	}
	for i, want := range wantRoles {
		if got := description.Nodes[i].Role; got != want {
			t.Fatalf("description node %d role = %q, want %q", i, got, want)
		}
	}
	if !description.Edges[0].ProgressSource {
		t.Fatal("demuxer output edge is not marked as progress source")
	}
	if got, want := description.Edges[3].Stream.Audio.SampleRate, 44103; got != want {
		t.Fatalf("encoder input edge sample rate = %d, want %d", got, want)
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
