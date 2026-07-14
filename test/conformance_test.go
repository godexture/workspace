package test

import (
	"testing"

	flacCodec "github.com/godexture/codec-flac"
	"github.com/godexture/codec-flac/test/config"
	"github.com/godexture/core/domain/media"
	flacFormat "github.com/godexture/format-flac"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
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
