package mp3

import (
	"testing"
	"time"
)

func BenchmarkSynthesizeGranule(b *testing.B) {
	for _, channels := range []int{1, 2} {
		name := "mono"
		if channels == 2 {
			name = "stereo"
		}
		b.Run(name, func(b *testing.B) {
			var granule [SamplesPerSubBandLayer3 * SubBandCount * MaxChannels]float32
			var pcm [SamplesPerSubBandLayer3 * SubBandCount * MaxChannels]float32
			var decoder Decoder
			decoder.Init()

			b.ReportAllocs()
			for b.Loop() {
				decoder.synthesizeGranule(granule[:], SamplesPerSubBandLayer3, channels, pcm[:])
			}
		})
	}
}

func BenchmarkSynthesizeGranulePaired(b *testing.B) {
	const batchSize = 64
	for _, channels := range []int{1, 2} {
		name := "mono"
		if channels == 2 {
			name = "stereo"
		}
		b.Run(name, func(b *testing.B) {
			var decoder Decoder
			decoder.Init()
			var referenceState [synthHistoryLength]float32
			var referenceWorkspace [2112]float32
			var granule [SamplesPerSubBandLayer3 * SubBandCount * MaxChannels]float32
			var pcm [SamplesPerSubBandLayer3 * SubBandCount * MaxChannels]float32
			var ringDuration, referenceDuration time.Duration
			iteration := 0

			for b.Loop() {
				runRing := func() {
					start := time.Now()
					for range batchSize {
						decoder.synthesizeGranule(granule[:], SamplesPerSubBandLayer3, channels, pcm[:])
					}
					ringDuration += time.Since(start)
				}
				runReference := func() {
					start := time.Now()
					for range batchSize {
						referenceSynthesizeGranule(referenceState[:], granule[:], SamplesPerSubBandLayer3, channels, pcm[:], referenceWorkspace[:])
					}
					referenceDuration += time.Since(start)
				}
				if iteration%2 == 0 {
					runRing()
					runReference()
				} else {
					runReference()
					runRing()
				}
				iteration++
			}

			operations := float64(b.N * batchSize)
			b.ReportMetric(float64(ringDuration.Nanoseconds())/operations, "ring-ns/op")
			b.ReportMetric(float64(referenceDuration.Nanoseconds())/operations, "reference-ns/op")
		})
	}
}
