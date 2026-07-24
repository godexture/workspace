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
)

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
