//go:generate go run ./snapshot-generator

package test

import (
	"testing"

	"github.com/godexture/codec-flac/test/bridge"
	"github.com/godexture/codec-flac/test/config"
	"github.com/godexture/sdk/testutil"
)

func TestFLACDecodeSnapshots(t *testing.T) {
	for _, fileName := range config.EnumerateTestdataFiles() {
		profile := config.ProfileForFile(fileName)
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()

			expectedPCM, err := testutil.LoadSnapshot(config.BuildSnapshotPath(fileName))
			if err != nil {
				t.Fatalf("failed to load expected PCM snapshot: %v", err)
			}

			testutil.RunSnapshotDecode(t, expectedPCM, config.BuildTestdataPath(fileName), profile.SnapshotCompareOptions, bridge.Decode)
		})
	}
}
