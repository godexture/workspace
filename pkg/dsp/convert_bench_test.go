package dsp

import "testing"

func BenchmarkConvertF32ToS16(b *testing.B) {
	source := make([]float32, 4096)
	for i := range source {
		source[i] = float32((i*7919)%131072-65536) / 32768
	}
	destination := make([]int16, len(source))
	b.ReportAllocs()
	b.SetBytes(int64(len(source) * 4))
	for b.Loop() {
		ConvertF32ToS16(destination, source)
	}
}
