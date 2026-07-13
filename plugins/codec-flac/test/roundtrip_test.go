package test

import (
	"os"
	"testing"

	"github.com/godexture/codec-flac/test/bridge"
	"github.com/godexture/codec-flac/test/config"
	"github.com/godexture/sdk/testutil"
)

func TestFLACRoundtripDecodeEncodeWAV(t *testing.T) {
	for _, fileName := range config.EnumerateTestdataFiles() {
		profile := config.ProfileForFile(fileName)
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()

			flacBytes, err := os.ReadFile(config.BuildTestdataPath(fileName))
			if err != nil {
				t.Fatalf("failed to read FLAC testdata: %v", err)
			}

			pcm, err := bridge.Decode(flacBytes)
			if err != nil {
				t.Fatalf("failed to decode FLAC testdata: %v", err)
			}

			attrs, err := bridge.AudioAttributes(flacBytes)
			if err != nil {
				t.Fatalf("failed to read FLAC audio attributes: %v", err)
			}

			encode := func(pcm []float32) ([]byte, error) {
				return bridge.EncodeWAV(pcm, attrs)
			}

			testutil.RunSnapshotEncode(t, pcm, profile.RoundtripCompareOptions, encode)
		})
	}
}
