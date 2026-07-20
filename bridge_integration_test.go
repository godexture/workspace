package filter

import (
	"testing"

	_ "github.com/godexture/codec-flac"
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/resolver"
)

func TestRegisteredBridgeSatisfiesFLACPCMInput(t *testing.T) {
	encoder := registeredFLACEncoder(t)
	requirements, err := encoder.Requirements(media.CodecFLAC, nil)
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
	if len(steps) != 1 || steps[0].Manifest.Name != "audio-convert" {
		t.Fatalf("bridge steps = %#v, want one audio-convert step", steps)
	}
	accepted, err := encoder.Accept(steps[0].Output, media.CodecFLAC, nil)
	if err != nil || !accepted {
		t.Fatalf("FLAC requirements accepted = %t, error = %v, output = %#v", accepted, err, steps[0].Output.Audio)
	}
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
