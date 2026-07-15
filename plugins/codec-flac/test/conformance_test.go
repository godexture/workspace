package test

import (
	"bytes"
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
	testAudio "github.com/godexture/sdk/testutil/audio"
)

func TestSnapshot(t *testing.T) {
	walkRoundtripFiles(t, func(t *testing.T, path string) {
		testutil.RunSnapshotTests(t, testutil.SnapshotConfig{
			MediaPath: path,
			Opts:      config.RoundtripCompareOptions,
			Demux:     flacFormat.NewDemuxerEngine,
			Decode: func(streamInfo media.StreamInfo) engine.DecoderEngine {
				return flacCodec.NewDecoderEngine(streamInfo, flacCodec.DecoderConfig{})
			},
			Encode: func() engine.EncoderEngine {
				encoder, _ := flacCodec.NewEncoderEngine(flacCodec.EncoderConfig{})
				return encoder
			},
			Mux: func(buf *testutil.Buffer) engine.MuxerEngine { return flacFormat.NewMuxerEngine(buf) },
		})
	})
}

// TestFaultyConformanceVectors verifies the vectors whose structural defects
// make rejection mandatory. The remaining faulty vectors are safety vectors:
// accepting or rejecting them is implementation-defined, but decoding must not
// panic.
func TestFaultyConformanceVectors(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(config.TestdataDir, "conformance", "faulty", "*.flac"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			_, err := decodeConformanceVector(path)
			if faultyMustReject(filepath.Base(path)) && err == nil {
				t.Fatal("faulty vector unexpectedly decoded successfully")
			}
		})
	}
}

// TestUncommonConformanceVectors exercises valid native FLAC streams whose
// frame properties change mid-stream. Files 10 and 11 deliberately omit a
// native FLAC container and are outside the demuxer contract.
func TestUncommonConformanceVectors(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(config.TestdataDir, "conformance", "uncommon", "*.flac"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		path := path
		if strings.HasPrefix(filepath.Base(path), "10 ") || strings.HasPrefix(filepath.Base(path), "11 ") {
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			if _, err := decodeConformanceVector(path); err != nil {
				t.Fatalf("uncommon vector failed to decode: %v", err)
			}
		})
	}
}

func decodeConformanceVector(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	demuxer, err := flacFormat.NewDemuxerEngine(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	streams, _, err := demuxer.Analyze()
	if err != nil {
		return nil, err
	}
	if len(streams) == 0 {
		return nil, engine.ErrEOF
	}

	packets, err := testAudio.EngineDemux(flacFormat.NewDemuxerEngine)(data)
	if err != nil {
		return nil, err
	}
	return testAudio.EngineDecode(streams[0], func(stream media.StreamInfo) engine.DecoderEngine {
		return flacCodec.NewDecoderEngine(stream, flacCodec.DecoderConfig{})
	})(packets)
}

func faultyMustReject(fileName string) bool {
	return strings.HasPrefix(fileName, "01 ") ||
		strings.HasPrefix(fileName, "06 ") ||
		strings.HasPrefix(fileName, "07 ") ||
		strings.HasPrefix(fileName, "08 ") ||
		strings.HasPrefix(fileName, "11 ")
}
