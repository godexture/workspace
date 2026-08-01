package gate

import (
	"math"
	"testing"
)

func TestOpennessTargetClampsAndInterpolates(t *testing.T) {
	t.Parallel()
	if got := opennessTarget(-10, -20, 40); got != 1 {
		t.Fatalf("above threshold = %v, want 1", got)
	}
	if got := opennessTarget(-70, -20, 40); got != 0 {
		t.Fatalf("far below threshold = %v, want 0", got)
	}
	if got := opennessTarget(-40, -20, 40); math.Abs(float64(got-0.5)) > 1e-9 {
		t.Fatalf("midpoint = %v, want 0.5", got)
	}
}

func TestOpennessTargetZeroRangeIsHardSwitch(t *testing.T) {
	t.Parallel()
	if got := opennessTarget(-19, -20, 0); got != 1 {
		t.Fatalf("above threshold with zero range = %v, want 1", got)
	}
	if got := opennessTarget(-21, -20, 0); got != 0 {
		t.Fatalf("below threshold with zero range = %v, want 0", got)
	}
}

func TestLogInterpEndpoints(t *testing.T) {
	t.Parallel()
	if got := logInterp(200, 20000, 0); math.Abs(got-200) > 1e-6 {
		t.Fatalf("logInterp(t=0) = %v, want 200", got)
	}
	if got := logInterp(200, 20000, 1); math.Abs(got-20000) > 1e-3 {
		t.Fatalf("logInterp(t=1) = %v, want 20000", got)
	}
}

func TestOnePoleCoeffIncreasesWithCutoff(t *testing.T) {
	t.Parallel()
	low := onePoleCoeff(200, 48000)
	high := onePoleCoeff(20000, 48000)
	if !(low > 0 && low < high && high < 1) {
		t.Fatalf("onePoleCoeff(200)=%v, onePoleCoeff(20000)=%v; want 0 < low < high < 1", low, high)
	}
}
