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
