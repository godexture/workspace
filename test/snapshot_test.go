//go:generate go run ./snapshot-generator

package test

import (
	"bytes"
	"context"
	"testing"

	mp3codec "github.com/godexture/codec-mp3"
	"github.com/godexture/codec-mp3/test/config"
	mp3format "github.com/godexture/format-mp3"
	"github.com/godexture/sdk/testutil"
)

var compareOption = testutil.CompareOptions{MaxAbsDiff: 1e-6, MaxRMSE: 1e-6, MinSNR: 100.0}

func Test_Snapshots(t *testing.T) {
	for _, fileName := range config.EnumerateTestdataFiles() {
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()

			dataPath := config.BuildTestdataPath(fileName)

			expectedPCM, err := testutil.LoadSnapshot(config.BuildSnapshotPath(fileName))
			if err != nil {
				t.Fatalf("failed to load expected PCM snapshot: %v", err)
			}

			testutil.RunSnapshotDecode(t, expectedPCM, dataPath, compareOption, decode)
		})
	}
}

func decode(data []byte) ([]float32, error) {
	demuxer, err := mp3format.NewDemuxerEngine(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	decoder := mp3codec.NewDecoderEngine(mp3codec.DecoderConfig{})
	return testutil.DecodeToFloat32(context.Background(), demuxer, decoder)
}
