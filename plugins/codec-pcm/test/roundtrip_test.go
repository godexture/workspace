package test

import (
	"os"
	"testing"

	"github.com/godexture/codec-pcm/test/bridge"
	"github.com/godexture/codec-pcm/test/config"
	"github.com/godexture/sdk/testutil"
)

func TestWavRoundtripDemuxDecodeEncodeMux(t *testing.T) {
	for _, profile := range config.Profiles {
		t.Run(profile.Name, func(t *testing.T) {
			t.Parallel()

			wavPath := config.BuildTestdataPath(profile.Name)

			encode := func(pcm []float32) ([]byte, error) {
				return bridge.Encode(pcm, profile.Codec, profile.Attrs)
			}
			testutil.RunRoundtripDecodeEncode(t, wavPath, profile.CompareOptions, bridge.Decode, encode)
		})
	}
}

func TestWavRoundtripEncodeMuxDemuxDecode(t *testing.T) {
	sourceWAV, err := os.Open(config.SourcePath)
	if err != nil {
		t.Fatalf("failed to open source WAV file: %v", err)
	}
	defer sourceWAV.Close()

	sourcePCM, err := testutil.DecodeWithFFmpeg(sourceWAV)
	if err != nil {
		t.Fatalf("failed to decode source WAV file: %v", err)
	}

	for _, profile := range config.Profiles {
		t.Run(profile.Name, func(t *testing.T) {
			t.Parallel()

			// Resample/reformat input PCM if necessary (e.g. downmix to mono for PCMU/PCMA)
			pcm := sourcePCM
			if profile.Attrs.ChannelLayout.ChannelCount() == 1 {
				pcm = testutil.DownmixToMono(pcm)
			}
			if profile.Attrs.SampleRate != 16000 {
				pcm = testutil.Resample16kTo8k(pcm, profile.Attrs.ChannelLayout.ChannelCount())
			}

			encode := func(pcm []float32) ([]byte, error) {
				return bridge.Encode(pcm, profile.Codec, profile.Attrs)
			}
			testutil.RunRoundtripEncodeDecode(t, pcm, profile.CompareOptions, encode, bridge.Decode)
		})
	}
}
