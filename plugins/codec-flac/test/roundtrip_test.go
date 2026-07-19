package test

import (
	"io"
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
			Demux: func(r io.ReadSeeker) (engine.DemuxerEngine, error) {
				return flacFormat.NewDemuxerEngine(r, flacFormat.NewDemuxerConfig(flacFormat.WithStrict(true)))
			},
			Decode: func(streamInfo media.StreamInfo) engine.DecoderEngine {
				streamInfo.Metadata = *metadata.NewBundle()
				dec, err := flacCodec.NewDecoderEngine(streamInfo, flacCodec.NewDecoderConfig(flacCodec.WithStrict(true)))
				if err != nil {
					t.Fatal(err)
				}
				return dec
			},
			Encode: func() engine.EncoderEngine {
				encoder, err := flacCodec.NewEncoderEngine(flacCodec.NewEncoderConfig())
				if err != nil {
					t.Fatal(err)
				}
				return encoder
			},
			Mux: func(w io.Writer) engine.MuxerEngine {
				mux, err := flacFormat.NewMuxerEngine(w, flacFormat.NewMuxerConfig())
				if err != nil {
					t.Fatal(err)
				}
				return mux
			},
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
