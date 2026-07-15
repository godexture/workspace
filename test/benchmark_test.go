package test

import (
	"os"
	"path/filepath"
	"testing"

	flacCodec "github.com/godexture/codec-flac"
	"github.com/godexture/core/domain/media"
	flacFormat "github.com/godexture/format-flac"
	"github.com/godexture/sdk/engine"
	"github.com/godexture/sdk/testutil"
)

func BenchmarkDecodeConformance(b *testing.B) {
	cases := []struct {
		name string
		path string
	}{
		{name: "SmallMono", path: filepath.Join("testdata", "60 - mono audio.flac")},
		{name: "Large384kHz", path: filepath.Join("testdata", "conformance", "subset", "36 - samplerate 384kHz.flac")},
	}
	for _, benchmark := range cases {
		b.Run(benchmark.name, func(b *testing.B) {
			info, err := os.Stat(benchmark.path)
			if err != nil {
				b.Fatal(err)
			}
			cfg := testutil.DecodeConfig{
				MediaPath: benchmark.path,
				Demux:     flacFormat.NewDemuxerEngine,
				Decode: func(stream media.StreamInfo) engine.DecoderEngine {
					return flacCodec.NewDecoderEngine(stream, flacCodec.DecoderConfig{})
				},
			}
			b.ReportAllocs()
			b.SetBytes(info.Size())
			b.ResetTimer()
			for b.Loop() {
				if err := testutil.RunDecode(b.Context(), cfg); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
