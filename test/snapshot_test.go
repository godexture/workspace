//go:generate go run ./snapshot-generator .

package test

import (
	"os"
	"testing"

	"github.com/godexture/codec-pcm/test/bridge"
	"github.com/godexture/codec-pcm/test/config"
	"github.com/godexture/sdk/testutil"
)

func TestWavDecodeSnapshots(t *testing.T) {
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

			dataPath := config.BuildTestdataPath(profile.Name)
			testutil.RunSnapshotDecode(t, sourcePCM, dataPath, profile.CompareOptions, bridge.Decode)
		})
	}
}

func TestWavEncodeSnapshots(t *testing.T) {
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

			encode := func(pcm []float32) ([]byte, error) {
				return bridge.Encode(pcm, profile.Codec, profile.Attrs)
			}
			testutil.RunSnapshotEncode(t, sourcePCM, profile.CompareOptions, encode)
		})
	}
}
