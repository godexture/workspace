package test

import (
	"io"
	"testing"

	"github.com/godexture/godec/plugin/mp3/test/config"
	mp3format "github.com/godexture/godec/plugin/mp3"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil"
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
					return mp3format.NewDemuxerEngine(r, mp3format.MustNewDemuxerConfig())
				},
				Mux: func(w io.Writer) engine.MuxerEngine {
					mux, err := mp3format.NewMuxerEngine(w, mp3format.MustNewMuxerConfig())
					if err != nil {
						t.Fatal(err)
					}
					return mux
				},
			})
		})
	}
}
