package test

import (
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	flacCodec "github.com/godexture/godec/plugin/flac"
	"github.com/godexture/godec/plugin/flac/test/config"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil"
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
				return flacCodec.NewDemuxerEngine(r, flacCodec.MustNewDemuxerConfig(flacCodec.WithDemuxerStrict(true)))
			},
			Decode: func(streamInfo media.StreamInfo) engine.DecoderEngine {
				streamInfo.Metadata = *metadata.NewBundle()
				dec, err := flacCodec.NewDecoderEngine(streamInfo, flacCodec.MustNewDecoderConfig(flacCodec.WithStrict(true)))
				if err != nil {
					t.Fatal(err)
				}
				return dec
			},
			Encode: func() engine.EncoderEngine {
				encoder, err := flacCodec.NewEncoderEngine(flacCodec.MustNewEncoderConfig())
				if err != nil {
					t.Fatal(err)
				}
				return encoder
			},
			Mux: func(w io.Writer) engine.MuxerEngine {
				mux, err := flacCodec.NewMuxerEngine(w, flacCodec.MustNewMuxerConfig())
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
