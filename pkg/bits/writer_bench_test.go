package bits

import "testing"

func BenchmarkWriterBits64Small(b *testing.B) {
	var writer Writer
	writer.Grow(8192)
	b.ReportAllocs()
	for b.Loop() {
		writer.Init()
		for i := uint64(0); i < 4096; i++ {
			width := uint8(i%16 + 1)
			writer.Bits64(i*0x9e3779b97f4a7c15, width)
		}
	}
}
