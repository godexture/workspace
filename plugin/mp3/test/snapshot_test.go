//go:generate go run ./snapshot-generator

package test

import (
	"io"
	"testing"

	mp3codec "github.com/godexture/godec/plugin/mp3"
	"github.com/godexture/godec/plugin/mp3/test/config"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil"
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
				Demux: func(r io.ReadSeeker) (engine.DemuxerEngine, error) {
					return mp3codec.NewDemuxerEngine(r, mp3codec.MustNewDemuxerConfig())
				},
				Decode: func(_ media.StreamInfo) engine.DecoderEngine {
					return mp3codec.NewDecoderEngine(mp3codec.MustNewDecoderConfig())
				},
			})
		})
	}
}
