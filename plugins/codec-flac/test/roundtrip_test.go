package test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	flacCodec "github.com/godexture/codec-flac"
	"github.com/godexture/codec-flac/test/config"
	"github.com/godexture/core/domain/media"
	flacFormat "github.com/godexture/format-flac"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

func TestRoundtrip(t *testing.T) {
	walkRoundtripFiles(t, func(t *testing.T, path string) {
		testutil.RunRoundtripTests(t, testutil.RoundtripConfig{
			MediaPath: path,
			Opts:      config.RoundtripCompareOptions,
			Demux:     flacFormat.NewDemuxerEngine,
			Decode: func(streamInfo media.StreamInfo) engine.DecoderEngine {
				return flacCodec.NewDecoderEngine(streamInfo, flacCodec.DecoderConfig{})
			},
			Encode: func() engine.EncoderEngine { return flacCodec.NewEncoderEngine(flacCodec.DefaultEncoderConfig) },
			Mux:    func(buf *testutil.Buffer) engine.MuxerEngine { return flacFormat.NewMuxerEngine(buf) },
		})
	})
}

func walkRoundtripFiles(t *testing.T, run func(t *testing.T, path string)) {
	root := config.TestdataDir
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".flac" {
			return nil
		}

		relPath, _ := filepath.Rel(root, path)
		testName := strings.ReplaceAll(relPath, string(os.PathSeparator), "/")

		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			run(t, path)
		})
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk testdata: %v", err)
	}
}
