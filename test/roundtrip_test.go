package test

import (
	"testing"

	"github.com/godexture/codec-mp3/test/config"
	mp3format "github.com/godexture/format-mp3"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

func TestRoundtrip(t *testing.T) {
	for _, fileName := range config.EnumerateTestdataFiles() {
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()

			dataPath := config.BuildTestdataPath(fileName)

			testutil.RunRoundtripTests(t, testutil.RoundtripConfig{
				MediaPath: dataPath,
				Demux:     mp3format.NewDemuxerEngine,
				Mux:       func(buf *testutil.Buffer) engine.MuxerEngine { return mp3format.NewMuxerEngine(buf) },
			})
		})
	}
}
