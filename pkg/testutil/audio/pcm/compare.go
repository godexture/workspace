package pcm

import (
	"fmt"
	"math"
)

type CompareOptions struct {
	MaxAbsDiff float32
	MaxRMSE    float32
	MinSNR     float32
}

type PCMStats struct {
	maxDiff         float32
	maxDiffIndex    int64
	maxDiffActual   float32
	maxDiffExpected float32
	sumSqDiff       float64
	sumSqSignal     float64
	comparedSamples int64
}

func (s *PCMStats) Add(actual, expected []float32) {
	for i := range actual {
		diff := float64(actual[i] - expected[i])
		absDiff := float32(math.Abs(diff))
		if absDiff > s.maxDiff {
			s.maxDiff = absDiff
			s.maxDiffIndex = s.comparedSamples + int64(i)
			s.maxDiffActual = actual[i]
			s.maxDiffExpected = expected[i]
		}
		s.sumSqDiff += diff * diff
		sig := float64(expected[i])
		s.sumSqSignal += sig * sig
	}
	s.comparedSamples += int64(len(actual))
}

func (s *PCMStats) Result(opts CompareOptions) error {
	if opts.MaxAbsDiff == 0 || opts.MaxRMSE == 0 || opts.MinSNR == 0 {
		return fmt.Errorf("invalid comparison options")
	}
	if s.maxDiff > opts.MaxAbsDiff {
		return fmt.Errorf("mismatch too high: max diff was %f at index %d (got %f, expected %f, allowed: %f)",
			s.maxDiff, s.maxDiffIndex, s.maxDiffActual, s.maxDiffExpected, opts.MaxAbsDiff)
	}
	if s.comparedSamples > 0 {
		rmse := float32(math.Sqrt(s.sumSqDiff / float64(s.comparedSamples)))
		if rmse > opts.MaxRMSE {
			return fmt.Errorf("RMSE too high: got %f, allowed: %f", rmse, opts.MaxRMSE)
		}
	}
	if s.sumSqDiff > 0 && s.sumSqSignal > 1e-5 {
		snr := float32(10 * (math.Log10(s.sumSqSignal) - math.Log10(s.sumSqDiff)))
		if snr < opts.MinSNR {
			return fmt.Errorf("SNR too low: got %f dB, required: %f dB", snr, opts.MinSNR)
		}
	}
	return nil
}
