package compressor

import (
	"math"
	"testing"
)

func TestGainReductionDBZeroBelowThreshold(t *testing.T) {
	t.Parallel()
	if got := gainReductionDB(-30, -18, 4, 6); got != 0 {
		t.Fatalf("gainReductionDB() = %v, want 0", got)
	}
}

func TestGainReductionDBHardKneeAboveThreshold(t *testing.T) {
	t.Parallel()
	got := gainReductionDB(0, -6, 4, 0)
	want := 6 * (1.0/4 - 1)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("gainReductionDB() = %v, want %v", got, want)
	}
}

func TestGainReductionDBContinuousAtKneeBoundaries(t *testing.T) {
	t.Parallel()
	threshold, ratio, knee := -18.0, 4.0, 6.0
	lower := gainReductionDB(threshold-knee/2, threshold, ratio, knee)
	if math.Abs(lower) > 1e-9 {
		t.Fatalf("reduction at lower knee edge = %v, want 0", lower)
	}
	upper := gainReductionDB(threshold+knee/2, threshold, ratio, knee)
	wantUpper := (knee / 2) * (1/ratio - 1)
	if math.Abs(upper-wantUpper) > 1e-9 {
		t.Fatalf("reduction at upper knee edge = %v, want %v", upper, wantUpper)
	}
}
