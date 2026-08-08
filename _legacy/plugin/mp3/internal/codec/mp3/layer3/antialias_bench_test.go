package layer3

import "testing"

func BenchmarkAntialias(b *testing.B) {
	granule := make([]float32, (SubBandCount+1)*SamplesPerSubBand)
	b.ReportAllocs()
	for b.Loop() {
		Antialias(granule, SubBandCount)
	}
}
