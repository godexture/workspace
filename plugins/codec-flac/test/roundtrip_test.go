package test

import (
	"os/exec"
	"strings"
	"testing"

	flacCodec "github.com/godexture/codec-flac"
	"github.com/godexture/codec-flac/test/config"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	flacFormat "github.com/godexture/format-flac"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

func TestRoundtrip(t *testing.T) {
	t.Parallel()
	walTestFiles(t, func(t *testing.T, path string, group string) {
		if strings.HasSuffix(group, "faulty") || strings.HasSuffix(group, "uncommon") {
			t.Skip("skipping faulty and uncommon conformance vectors in snapshot test")
			return
		}

		testutil.RunRoundtripTests(t, testutil.RoundtripConfig{
			MediaPath: path,
			Opts:      config.RoundtripCompareOptions,
			Demux:     flacFormat.NewDemuxerEngine,
			Decode: func(streamInfo media.StreamInfo) engine.DecoderEngine {
				streamInfo.Metadata = *metadata.NewBundle()
				return flacCodec.NewDecoderEngine(streamInfo, flacCodec.DecoderConfig{})
			},
			Encode: func() engine.EncoderEngine {
				encoder, _ := flacCodec.NewEncoderEngine(flacCodec.EncoderConfig{})
				return encoder
			},
			Mux:    flacFormat.NewMuxerEngine,
			Tester: testFLAC,
		})
	})
}

func testFLAC(t testing.TB, path string) {
	t.Helper()

	output, err := exec.CommandContext(t.Context(), "flac", "-t", path).CombinedOutput()
	if err != nil {
		t.Errorf("flac -t %s: %v\n%s", path, err, output)
	}
}
