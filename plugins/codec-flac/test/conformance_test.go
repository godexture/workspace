package test

import (
	"io"
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

func TestSnapshot(t *testing.T) {
	t.Parallel()
	walTestFiles(t, func(t *testing.T, path string, group string) {
		if strings.HasSuffix(group, "faulty") || strings.HasSuffix(group, "uncommon") {
			t.Skip("skipping faulty and uncommon conformance vectors in snapshot test")
			return
		}

		testutil.RunSnapshotTests(t, testutil.SnapshotConfig{
			MediaPath: path,
			Opts:      config.RoundtripCompareOptions,
			Demux: func(r io.ReadSeeker) (engine.DemuxerEngine, error) {
				return flacFormat.NewDemuxerEngine(r, flacFormat.NewDemuxerConfig())
			},
			Decode: func(streamInfo media.StreamInfo) engine.DecoderEngine {
				dec, err := flacCodec.NewDecoderEngine(streamInfo, flacCodec.NewDecoderConfig())
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

func TestConformance(t *testing.T) {
	t.Parallel()
	walTestFiles(t, func(t *testing.T, path string, group string) {
		if strings.HasSuffix(group, "uncommon") && uncommonNotSupported(filepath.Base(path)) {
			t.Skip("skipping unsupported uncommon conformance vector")
			return
		}

		err := decodeConformanceVector(t, path)

		if strings.HasSuffix(group, "faulty") {
			if faultyMustReject(filepath.Base(path)) && err == nil {
				t.Errorf("expected conformance vector to be rejected: %s", filepath.Base(path))
			}
		} else if err != nil {
			t.Errorf("failed to decode conformance vector: %s, error: %v", filepath.Base(path), err)
		}
	})
}

func faultyMustReject(fileName string) bool {
	return strings.HasPrefix(fileName, "08 ") ||
		strings.HasPrefix(fileName, "11 ")
}

func uncommonNotSupported(fileName string) bool {
	return strings.HasPrefix(fileName, "10 ") || strings.HasPrefix(fileName, "11 ")
}

func decodeConformanceVector(t *testing.T, path string) error {
	t.Helper()

	return testutil.RunDecode(t.Context(), testutil.DecodeConfig{
		MediaPath: path,
		Demux: func(r io.ReadSeeker) (engine.DemuxerEngine, error) {
			return flacFormat.NewDemuxerEngine(r, flacFormat.NewDemuxerConfig())
		},
		Decode: func(stream media.StreamInfo) engine.DecoderEngine {
			dec, err := flacCodec.NewDecoderEngine(stream, flacCodec.NewDecoderConfig())
			if err != nil {
				t.Fatal(err)
			}
			return dec
		},
	})
}
