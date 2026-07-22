package config

import (
	"testing"

	"github.com/godexture/core/domain/media"
)

func TestFormatConfigEffectiveBitsPerSample(t *testing.T) {
	t.Parallel()
	if got, want := (FormatConfig{Format: media.SampleFormatS24}).EffectiveBitsPerSample(), 24; got != want {
		t.Fatalf("default effective bits = %d, want %d", got, want)
	}
	if got, want := (FormatConfig{Format: media.SampleFormatS32, BitsPerSample: 20}).EffectiveBitsPerSample(), 20; got != want {
		t.Fatalf("explicit effective bits = %d, want %d", got, want)
	}
}

func TestSpeedConfigValidate(t *testing.T) {
	t.Parallel()
	if err := (SpeedConfig{Factor: 0, Mode: SpeedModeInterpolate}).Validate(); err == nil {
		t.Fatal("want error for non-positive factor")
	}
	if err := (SpeedConfig{Factor: 2, Mode: SpeedModeInterpolate}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (SpeedConfig{Factor: 2, Mode: SpeedModeRelabel}).Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (SpeedConfig{Factor: 2, Mode: "bogus"}).Validate(); err == nil {
		t.Fatal("want error for invalid mode")
	}
}

func TestGateConfigValidate(t *testing.T) {
	t.Parallel()
	hard := GateConfig{ThresholdDBFS: -60, GateMode: GateModeHard}
	if err := hard.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() GateConfig { c := hard; c.ThresholdDBFS = 1; return c }()).Validate(); err == nil {
		t.Fatal("want error for positive threshold")
	}
	if err := (func() GateConfig { c := hard; c.GateMode = "bogus"; return c }()).Validate(); err == nil {
		t.Fatal("want error for invalid mode")
	}

	lowpass := GateConfig{ThresholdDBFS: -60, GateMode: GateModeLowpass, RangeDB: 40, AttackMs: 5, ReleaseMs: 50, OpenFrequencyHz: 20000, CloseFrequencyHz: 200}
	if err := lowpass.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() GateConfig { c := lowpass; c.RangeDB = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative range")
	}
	if err := (func() GateConfig { c := lowpass; c.CloseFrequencyHz = 30000; return c }()).Validate(); err == nil {
		t.Fatal("want error for close frequency above open frequency")
	}
	if err := (GateConfig{GateMode: GateModeLowpass}).Validate(); err == nil {
		t.Fatal("want error for zero-value open/close frequencies")
	}
}

func TestCompressorConfigValidate(t *testing.T) {
	t.Parallel()
	valid := CompressorConfig{ThresholdDBFS: -18, Ratio: 4, AttackMs: 10, ReleaseMs: 100, KneeDB: 6}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() CompressorConfig { c := valid; c.ThresholdDBFS = 1; return c }()).Validate(); err == nil {
		t.Fatal("want error for positive threshold")
	}
	if err := (func() CompressorConfig { c := valid; c.Ratio = 0.5; return c }()).Validate(); err == nil {
		t.Fatal("want error for ratio below 1")
	}
	if err := (func() CompressorConfig { c := valid; c.AttackMs = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative attack")
	}
	if err := (func() CompressorConfig { c := valid; c.KneeDB = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative knee")
	}
}

func TestEQConfigValidate(t *testing.T) {
	t.Parallel()
	valid := EQConfig{Type: EQTypePeaking, FrequencyHz: 1000, Q: 0.707}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() EQConfig { c := valid; c.Type = "bogus"; return c }()).Validate(); err == nil {
		t.Fatal("want error for invalid type")
	}
	if err := (func() EQConfig { c := valid; c.FrequencyHz = 0; return c }()).Validate(); err == nil {
		t.Fatal("want error for non-positive frequency")
	}
	if err := (func() EQConfig { c := valid; c.Q = 0; return c }()).Validate(); err == nil {
		t.Fatal("want error for non-positive Q")
	}
}
