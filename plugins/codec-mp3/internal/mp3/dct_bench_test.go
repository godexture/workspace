package mp3

import "testing"

func BenchmarkDCTType2(b *testing.B) {
	for _, name := range []struct {
		name      string
		bandCount int
	}{
		{"layer3", SamplesPerSubBandLayer3},
		{"layer12", SamplesPerSubBandLayer12},
	} {
		b.Run(name.name, func(b *testing.B) {
			granule := make([]float32, SamplesPerSubBandLayer3*SubBandCount)
			for i := range granule {
				granule[i] = float32(i%31-15) / 16
			}
			b.ReportAllocs()
			for b.Loop() {
				dctType2(granule, name.bandCount)
			}
		})
	}
}
