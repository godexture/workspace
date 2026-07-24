package equalizer

import (
	"math"
	"testing"

	"github.com/godexture/filter-audio/internal/config"
)

func TestComputeBiquadLowPassUnityGainAtDC(t *testing.T) {
	t.Parallel()
	c := computeBiquad(config.EqualizerConfig{Type: config.EqualizerTypeLowPass, FrequencyHz: 1000, Q: 0.7071067811865476}, 48000)
	assertNear(t, float64(c.b0+c.b1+c.b2), float64(1+c.a1+c.a2), 1e-5, "lowpass DC gain")
}

func TestComputeBiquadHighPassZeroGainAtDC(t *testing.T) {
	t.Parallel()
	c := computeBiquad(config.EqualizerConfig{Type: config.EqualizerTypeHighPass, FrequencyHz: 1000, Q: 0.7071067811865476}, 48000)
	assertNear(t, float64(c.b0+c.b1+c.b2), 0, 1e-5, "highpass DC gain numerator")
}

func TestComputeBiquadPeakingZeroGainIsAllPass(t *testing.T) {
	t.Parallel()
	c := computeBiquad(config.EqualizerConfig{Type: config.EqualizerTypePeaking, FrequencyHz: 1000, GainDB: 0, Q: 0.7071067811865476}, 48000)
	assertNear(t, float64(c.b0), 1, 1e-6, "peaking b0")
	assertNear(t, float64(c.b1), float64(c.a1), 1e-6, "peaking b1 vs a1")
	assertNear(t, float64(c.b2), float64(c.a2), 1e-6, "peaking b2 vs a2")
}

func assertNear(t *testing.T, got, want, tol float64, what string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s = %g, want %g (tol %g)", what, got, want, tol)
	}
}
