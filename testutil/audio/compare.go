package audio

import (
	"fmt"
	"math"
)

type CompareOptions struct {
	MaxAbsDiff float32
	MaxRMSE    float32
	MinSNR     float32
}

// ComparePCM compares actual and expected float32 PCM samples and returns an error if the difference exceeds options criteria.
func ComparePCM(actual, expected []float32, opts CompareOptions) error {
	if opts.MaxAbsDiff == 0 || opts.MaxRMSE == 0 || opts.MinSNR == 0 {
		return fmt.Errorf("invalid comparison options")
	}

	var (
		maxDiff      float32 = 0
		maxDiffIndex int     = -1
		sumSqDiff    float64 = 0
		sumSqSignal  float64 = 0
	)

	fmt.Printf("Comparing PCM: actual length=%d, expected length=%d\n", len(actual), len(expected))

	n := min(len(actual), len(expected))
	for i := 0; i < n; i++ {
		diff := float64(actual[i] - expected[i])
		absDiff := float32(math.Abs(diff))
		if absDiff > maxDiff {
			maxDiff = absDiff
			maxDiffIndex = i
		}
		sumSqDiff += diff * diff
		sig := float64(expected[i])
		sumSqSignal += sig * sig
	}

	if maxDiff > opts.MaxAbsDiff {
		return fmt.Errorf("mismatch too high: max diff was %f at index %d (got %f, expected %f, allowed: %f)",
			maxDiff, maxDiffIndex, actual[maxDiffIndex], expected[maxDiffIndex], opts.MaxAbsDiff)
	}

	if n > 0 {
		rmse := float32(math.Sqrt(sumSqDiff / float64(n)))
		if rmse > opts.MaxRMSE {
			return fmt.Errorf("RMSE too high: got %f, allowed: %f", rmse, opts.MaxRMSE)
		}
	}

	if sumSqDiff > 0 && sumSqSignal > 1e-5 {
		snr := float32(10 * (math.Log10(sumSqSignal) - math.Log10(sumSqDiff)))
		if snr < opts.MinSNR {
			return fmt.Errorf("SNR too low: got %f dB, required: %f dB", snr, opts.MinSNR)
		}
	}

	return nil
}
