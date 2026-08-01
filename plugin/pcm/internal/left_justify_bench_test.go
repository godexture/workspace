package internal

import (
	"testing"

	"github.com/godexture/godec/core/domain/media"
)

func BenchmarkLeftJustifyPCM(b *testing.B) {
	tests := []struct {
		name          string
		format        media.SampleFormat
		bitsPerSample int
		size          int
	}{
		{name: "s16", format: media.SampleFormatS16, bitsPerSample: 12, size: 4096 * 2},
		{name: "s24", format: media.SampleFormatS24, bitsPerSample: 20, size: 4096 * 3},
		{name: "s32", format: media.SampleFormatS32, bitsPerSample: 24, size: 4096 * 4},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			data := make([]byte, test.size)
			var scratch []byte
			b.ReportAllocs()
			b.SetBytes(int64(test.size))
			for b.Loop() {
				_ = leftJustifyPCM(&scratch, data, test.format, test.bitsPerSample)
			}
		})
	}
}
