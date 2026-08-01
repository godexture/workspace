package filter

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"

	_ "github.com/godexture/godec/plugin/pcm"
	godec "github.com/godexture/godec/core"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/core/resolver"
	"github.com/godexture/godec/core/routing"
	formatFlac "github.com/godexture/godec/plugin/flac"
	formatWav "github.com/godexture/godec/plugin/wave"
)

func TestRegisteredBridgeSatisfiesFLACPCMInput(t *testing.T) {
	encoder := registeredFLACEncoder(t)
	requirements, err := encoder.Requirements("in", media.CodecFLAC, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := media.StreamInfo{
		Type: media.MediaAudio,
		MediaAttributes: media.MediaAttributes{
			Codec: media.CodecLPCM,
			Audio: media.AudioAttributes{
				SampleRate:    44100,
				Format:        media.SampleFormatF32P,
				BitsPerSample: 32,
				ChannelLayout: media.LayoutStereo2_0,
			},
		},
	}
	steps, err := resolver.NewDefaultBridgeResolver(godec.DefaultFilterRegistry).ResolveBridge(source, requirements)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Manifest.Name != "convert" {
		t.Fatalf("bridge steps = %#v, want one convert step", steps)
	}
	accepted, err := encoder.Accept("in", steps[0].Output, media.CodecFLAC, nil)
	if err != nil || !accepted {
		t.Fatalf("FLAC requirements accepted = %t, error = %v, output = %#v", accepted, err, steps[0].Output.Audio)
	}
}

func TestAutomaticBridgeConvertsWAVFloatToFLAC(t *testing.T) {
	input := makeFloatWAV(t, []float32{0, .25, -.25, .5})
	var output bytes.Buffer
	geometry, err := godec.NewNegotiator().NegotiateConversion(context.Background(), routing.ConversionSpec{
		Input:       bytes.NewReader(input),
		Output:      &output,
		TargetCodec: media.CodecFLAC,
		MuxConfig:   formatFlac.MustNewMuxerConfig(),
	})
	if err != nil {
		t.Fatal(err)
	}
	conversion, err := godec.NewBuilder().Build(geometry)
	if err != nil {
		t.Fatal(err)
	}
	defer conversion.Close()
	if err := conversion.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(output.Bytes(), []byte("fLaC")) {
		t.Fatalf("output does not have FLAC signature: % x", output.Bytes())
	}
}

func makeFloatWAV(t *testing.T, samples []float32) []byte {
	t.Helper()
	var result bytes.Buffer
	muxer, err := formatWav.NewMuxerEngine(&result, formatWav.MustNewMuxerConfig())
	if err != nil {
		t.Fatal(err)
	}
	stream := media.StreamInfo{Type: media.MediaAudio, MediaAttributes: media.MediaAttributes{
		Codec: media.CodecLPCM,
		Audio: media.AudioAttributes{SampleRate: 44100, Format: media.SampleFormatF32, BitsPerSample: 32, ChannelLayout: media.LayoutMono1},
	}}
	if _, err := muxer.AddStream(stream); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	packet := media.NewPacket(len(samples) * 4)
	for i, sample := range samples {
		binary.LittleEndian.PutUint32(packet.Data()[i*4:], math.Float32bits(sample))
	}
	if err := muxer.WritePacket(0, packet); err != nil {
		t.Fatal(err)
	}
	packet.Release()
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func registeredFLACEncoder(t *testing.T) registry.EncoderManifest {
	t.Helper()
	for manifest := range godec.DefaultEncoderRegistry.Enumerate() {
		if manifest.Supports(media.CodecFLAC) {
			return manifest
		}
	}
	t.Fatal("FLAC encoder is not registered")
	return registry.EncoderManifest{}
}
