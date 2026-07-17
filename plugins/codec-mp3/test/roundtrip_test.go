package test

import (
	"io"
	"testing"

	"github.com/godexture/codec-mp3/test/config"
	mp3format "github.com/godexture/format-mp3"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

func TestRoundtrip(t *testing.T) {
	t.Parallel()
	for _, fileName := range config.EnumerateTestdataFiles() {
		t.Run(fileName, func(t *testing.T) {
			t.Parallel()

			dataPath := config.BuildTestdataPath(fileName)

			testutil.RunRoundtripTests(t, testutil.RoundtripConfig{
				MediaPath: dataPath,
				Demux: func(r io.ReadSeeker) (engine.DemuxerEngine, error) {
					return mp3format.NewDemuxerEngine(r, mp3format.DemuxerConfig{})
				},
				Mux: func(w io.Writer) engine.MuxerEngine {
					return mp3format.NewMuxerEngine(w, mp3format.MuxerConfig{})
				},
			})
		})
	}
}
