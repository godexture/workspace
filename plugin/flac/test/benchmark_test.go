package test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/godexture/godec/core/domain/media"
	flacCodec "github.com/godexture/godec/plugin/flac"
	"github.com/godexture/godec/sdk/engine"
	"github.com/godexture/godec/sdk/testutil"
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
				Demux: func(r io.ReadSeeker) (engine.DemuxerEngine, error) {
					return flacCodec.NewDemuxerEngine(r, flacCodec.MustNewDemuxerConfig(flacCodec.WithDemuxerStrict(true)))
				},
				Decode: func(stream media.StreamInfo) engine.DecoderEngine {
					dec, err := flacCodec.NewDecoderEngine(stream, flacCodec.MustNewDecoderConfig())
					if err != nil {
						b.Fatal(err)
					}
					return dec
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
