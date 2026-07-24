package routing

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/resolver"
)

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
