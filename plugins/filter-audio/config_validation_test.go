package filter_test

import (
	"testing"
	"github.com/godexture/filter-audio"
)

func TestSpeedConfigValidate(t *testing.T) {
	t.Parallel()
	if err := (filter.SpeedConfig{Factor: 0, Mode: filter.SpeedModeInterpolate}).Validate(); err == nil {
		t.Fatal("want error for non-positive factor")
	}
	if err := (filter.SpeedConfig{Factor: 2, Mode: filter.SpeedModeInterpolate}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (filter.SpeedConfig{Factor: 2, Mode: filter.SpeedModeRelabel}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (filter.SpeedConfig{Factor: 2, Mode: "bogus"}).Validate(); err == nil {
		t.Fatal("want error for invalid mode")
	}
}

func TestGateConfigValidate(t *testing.T) {
	t.Parallel()
	hard := filter.GateConfig{ThresholdDBFS: -60, GateMode: filter.GateModeHard}
	if err := hard.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() filter.GateConfig { c := hard; c.ThresholdDBFS = 1; return c }()).Validate(); err == nil {
		t.Fatal("want error for positive threshold")
	}
	if err := (func() filter.GateConfig { c := hard; c.GateMode = "bogus"; return c }()).Validate(); err == nil {
		t.Fatal("want error for invalid mode")
	}

	lowpass := filter.GateConfig{ThresholdDBFS: -60, GateMode: filter.GateModeLowpass, RangeDB: 40, AttackMs: 5, ReleaseMs: 50, OpenFrequencyHz: 20000, CloseFrequencyHz: 200}
	if err := lowpass.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() filter.GateConfig { c := lowpass; c.RangeDB = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative range")
	}
	if err := (func() filter.GateConfig { c := lowpass; c.CloseFrequencyHz = 30000; return c }()).Validate(); err == nil {
		t.Fatal("want error for close frequency above open frequency")
	}
	if err := (filter.GateConfig{GateMode: filter.GateModeLowpass}).Validate(); err == nil {
		t.Fatal("want error for zero-value open/close frequencies")
	}
}

func TestCompressorConfigValidate(t *testing.T) {
	t.Parallel()
	valid := filter.CompressorConfig{ThresholdDBFS: -18, Ratio: 4, AttackMs: 10, ReleaseMs: 100, KneeDB: 6}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() filter.CompressorConfig { c := valid; c.ThresholdDBFS = 1; return c }()).Validate(); err == nil {
		t.Fatal("want error for positive threshold")
	}
	if err := (func() filter.CompressorConfig { c := valid; c.Ratio = 0.5; return c }()).Validate(); err == nil {
		t.Fatal("want error for ratio below 1")
	}
	if err := (func() filter.CompressorConfig { c := valid; c.AttackMs = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative attack")
	}
	if err := (func() filter.CompressorConfig { c := valid; c.KneeDB = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative knee")
	}
}

func TestEqualizerConfigValidate(t *testing.T) {
	t.Parallel()
	valid := filter.MustNewEqualizerConfig()
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() filter.EqualizerConfig { c := valid; c.Type = "bogus"; return c }()).Validate(); err == nil {
		t.Fatal("want error for invalid type")
	}
	if err := (func() filter.EqualizerConfig { c := valid; c.FrequencyHz = -5; return c }()).Validate(); err == nil {
		t.Fatal("want error for non-positive frequency")
	}
	if err := (func() filter.EqualizerConfig { c := valid; c.Q = 0; return c }()).Validate(); err == nil {
		t.Fatal("want error for non-positive Q")
	}
	multiband := valid
	multiband.EqualizerMode = filter.EqualizerModeMultiband
	if err := multiband.Validate(); err != nil {
		t.Fatalf("multiband Validate() error = %v", err)
	}
	if err := (func() filter.EqualizerConfig { c := multiband; c.Gains = "0"; return c }()).Validate(); err == nil {
		t.Fatal("want error for gains/band count mismatch")
	}
	if err := (func() filter.EqualizerConfig { c := multiband; c.LowHz = c.HighHz; return c }()).Validate(); err == nil {
		t.Fatal("want error for invalid automatic range")
	}
	if err := (func() filter.EqualizerConfig { c := multiband; c.ManualBands = "100,0"; c.Gains = "0,0"; return c }()).Validate(); err == nil {
		t.Fatal("want error for non-positive manual frequency")
	}
	if err := (func() filter.EqualizerConfig { c := multiband; c.EqualizerMode = "bogus"; return c }()).Validate(); err == nil {
		t.Fatal("want error for invalid mode")
	}
	if err := (func() filter.EqualizerConfig { c := multiband; c.ManualBands = "100,abc"; c.Gains = "0,0"; return c }()).Validate(); err == nil {
		t.Fatal("want error for malformed manual bands")
	}
	if err := (func() filter.EqualizerConfig { c := multiband; c.Gains = "0,abc"; return c }()).Validate(); err == nil {
		t.Fatal("want error for malformed gains")
	}
}

func TestConvolutionConfigValidate(t *testing.T) {
	t.Parallel()
	valid := filter.ConvolutionConfig{ImpulseResponse: [][]float32{{1, 0.5}}, WetDryMix: 1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() filter.ConvolutionConfig { c := valid; c.ImpulseResponse = nil; return c }()).Validate(); err != nil {
		t.Fatalf("port-fed impulse response config rejected: %v", err)
	}
	if err := (func() filter.ConvolutionConfig { c := valid; c.ImpulseResponse = [][]float32{{}}; return c }()).Validate(); err == nil {
		t.Fatal("want error for empty impulse response channel")
	}
	if err := (func() filter.ConvolutionConfig {
		c := valid
		c.ImpulseResponse = [][]float32{{1, 0.5}, {1}}
		return c
	}()).Validate(); err == nil {
		t.Fatal("want error for mismatched impulse response channel lengths")
	}
	if err := (func() filter.ConvolutionConfig { c := valid; c.ImpulseRate = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative impulse rate")
	}
	if err := (func() filter.ConvolutionConfig { c := valid; c.WetDryMix = 1.5; return c }()).Validate(); err == nil {
		t.Fatal("want error for wet/dry mix above 1")
	}
	if err := (func() filter.ConvolutionConfig { c := valid; c.BlockSize = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative block size")
	}
}

func TestDelayConfigValidate(t *testing.T) {
	t.Parallel()
	valid := filter.DelayConfig{DelayMs: 300, Feedback: 0.3, WetLevel: 0.5, DryLevel: 1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() filter.DelayConfig { c := valid; c.DelayMs = 0; return c }()).Validate(); err == nil {
		t.Fatal("want error for non-positive delay time")
	}
	if err := (func() filter.DelayConfig { c := valid; c.Feedback = -0.1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative feedback")
	}
	if err := (func() filter.DelayConfig { c := valid; c.Feedback = 1; return c }()).Validate(); err == nil {
		t.Fatal("want error for feedback at unity (unstable)")
	}
	if err := (func() filter.DelayConfig { c := valid; c.WetLevel = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative wet level")
	}
	if err := (func() filter.DelayConfig { c := valid; c.DryLevel = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative dry level")
	}
}

func TestReverbConfigValidate(t *testing.T) {
	t.Parallel()
	valid := filter.ReverbConfig{RoomSize: 0.5, Damping: 0.5, WetLevel: 0.3, DryLevel: 1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() filter.ReverbConfig { c := valid; c.RoomSize = -0.1; return c }()).Validate(); err == nil {
		t.Fatal("want error for room size below 0")
	}
	if err := (func() filter.ReverbConfig { c := valid; c.RoomSize = 1.1; return c }()).Validate(); err == nil {
		t.Fatal("want error for room size above 1")
	}
	if err := (func() filter.ReverbConfig { c := valid; c.Damping = 1.1; return c }()).Validate(); err == nil {
		t.Fatal("want error for damping above 1")
	}
	if err := (func() filter.ReverbConfig { c := valid; c.WetLevel = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative wet level")
	}
	if err := (func() filter.ReverbConfig { c := valid; c.DryLevel = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative dry level")
	}
}