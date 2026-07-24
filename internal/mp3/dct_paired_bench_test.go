//go:build goexperiment.simd && amd64

package mp3

import (
	"testing"
	"time"
)

// BenchmarkDCTType2Paired cross-checks BenchmarkDCTType2Compare with the same
// interleaved-measurement technique BenchmarkSynthesizeGranulePaired uses:
// this machine's clock throttles noticeably over a sustained run (confirmed
// by BenchmarkDCTType2Compare swinging from ~1200ns to ~3400ns for the same
// scalar code across consecutive sub-benchmarks), so alternating which
// variant goes first each iteration is needed to avoid being fooled by
// drift instead of measuring the real difference.
func BenchmarkDCTType2Paired(b *testing.B) {
	granule := make([]float32, SamplesPerSubBandLayer3*SubBandCount)
	for i := range granule {
		granule[i] = float32(i%31-15) / 16
	}

	const batchSize = 64
	var scalarDuration, simdDuration time.Duration
	iteration := 0

	for b.Loop() {
		runScalar := func() {
			start := time.Now()
			for range batchSize {
				dctType2Scalar(granule, SamplesPerSubBandLayer3)
			}
			scalarDuration += time.Since(start)
		}
		runSIMD := func() {
			start := time.Now()
			for range batchSize {
				dctType2SIMD(granule, SamplesPerSubBandLayer3)
			}
			simdDuration += time.Since(start)
		}
		if iteration%2 == 0 {
			runScalar()
			runSIMD()
		} else {
			runSIMD()
			runScalar()
		}
		iteration++
	}

	operations := float64(b.N * batchSize)
	b.ReportMetric(float64(scalarDuration.Nanoseconds())/operations, "scalar-ns/op")
	b.ReportMetric(float64(simdDuration.Nanoseconds())/operations, "simd-ns/op")
}
