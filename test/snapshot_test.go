//go:generate go run ./snapshot-generator

package test

import (
	"testing"

	mp3codec "github.com/godexture/codec-mp3"
	"github.com/godexture/codec-mp3/test/config"
	"github.com/godexture/core/domain/media"
	mp3format "github.com/godexture/format-mp3"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

var compareOption = testutil.CompareOptions{MaxAbsDiff: 1e-6, MaxRMSE: 1e-6, MinSNR: 100.0}

func TestSnapshots(t *testing.T) {
	t.Parallel()
	for _, fileName := range config.EnumerateTestdataFiles() {
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()

			dataPath := config.BuildTestdataPath(fileName)
			snapshotPath := config.BuildSnapshotPath(fileName)

			snapshot, err := testutil.LoadSnapshot(snapshotPath)
			if err != nil {
				t.Fatalf("failed to load snapshot: %v", err)
			}

			testutil.RunSnapshotTests(t, testutil.SnapshotConfig{
				MediaPath: dataPath,
				Expected:  snapshot,
				Opts:      compareOption,
				Demux:     mp3format.NewDemuxerEngine,
				Decode: func(_ media.StreamInfo) engine.DecoderEngine {
					return mp3codec.NewDecoderEngine(mp3codec.DecoderConfig{})
				},
			})
		})
	}
}
