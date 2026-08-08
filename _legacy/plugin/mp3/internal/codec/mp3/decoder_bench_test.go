package mp3

import (
	"errors"
	"os"
	"testing"

	"github.com/godexture/godec/plugin/mp3/internal/codec/mp3/domain"
)

func BenchmarkDecodeFile(b *testing.B) {
	data, err := os.ReadFile("../../../test/testdata/l3-sin1k0db.mp3")
	if err != nil {
		b.Fatal(err)
	}
	pcm := make([]float32, SamplesPerFrameLayer23*MaxChannels)

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		var decoder Decoder
		decoder.Init()
		for offset := 0; offset < len(data); {
			_, info, err := decoder.DecodeFrame(data[offset:], pcm)
			if err != nil && !errors.Is(err, domain.ErrInsufficientReservoir) {
				b.Fatal(err)
			}
			if info.FrameBytes <= 0 {
				break
			}
			offset += info.FrameBytes
		}
	}
}
