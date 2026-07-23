package config

import (
	"math"
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

func TestConvolutionConfigValidate(t *testing.T) {
	t.Parallel()
	valid := ConvolutionConfig{ImpulseResponse: [][]float32{{1, 0.5}}, WetDryMix: 1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() ConvolutionConfig { c := valid; c.ImpulseResponse = nil; return c }()).Validate(); err != nil {
		t.Fatalf("port-fed impulse response config rejected: %v", err)
	}
	if err := (func() ConvolutionConfig { c := valid; c.ImpulseResponse = [][]float32{{}}; return c }()).Validate(); err == nil {
		t.Fatal("want error for empty impulse response channel")
	}
	if err := (func() ConvolutionConfig {
		c := valid
		c.ImpulseResponse = [][]float32{{1, 0.5}, {1}}
		return c
	}()).Validate(); err == nil {
		t.Fatal("want error for mismatched impulse response channel lengths")
	}
	if err := (func() ConvolutionConfig { c := valid; c.ImpulseRate = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative impulse rate")
	}
	if err := (func() ConvolutionConfig { c := valid; c.WetDryMix = 1.5; return c }()).Validate(); err == nil {
		t.Fatal("want error for wet/dry mix above 1")
	}
	if err := (func() ConvolutionConfig { c := valid; c.BlockSize = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative block size")
	}
}

func TestDelayConfigValidate(t *testing.T) {
	t.Parallel()
	valid := DelayConfig{DelayMs: 300, Feedback: 0.3, WetLevel: 0.5, DryLevel: 1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() DelayConfig { c := valid; c.DelayMs = 0; return c }()).Validate(); err == nil {
		t.Fatal("want error for non-positive delay time")
	}
	if err := (func() DelayConfig { c := valid; c.Feedback = -0.1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative feedback")
	}
	if err := (func() DelayConfig { c := valid; c.Feedback = 1; return c }()).Validate(); err == nil {
		t.Fatal("want error for feedback at unity (unstable)")
	}
	if err := (func() DelayConfig { c := valid; c.WetLevel = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative wet level")
	}
	if err := (func() DelayConfig { c := valid; c.DryLevel = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative dry level")
	}
}

func TestReverbConfigValidate(t *testing.T) {
	t.Parallel()
	valid := ReverbConfig{RoomSize: 0.5, Damping: 0.5, WetLevel: 0.3, DryLevel: 1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (func() ReverbConfig { c := valid; c.RoomSize = -0.1; return c }()).Validate(); err == nil {
		t.Fatal("want error for room size below 0")
	}
	if err := (func() ReverbConfig { c := valid; c.RoomSize = 1.1; return c }()).Validate(); err == nil {
		t.Fatal("want error for room size above 1")
	}
	if err := (func() ReverbConfig { c := valid; c.Damping = 1.1; return c }()).Validate(); err == nil {
		t.Fatal("want error for damping above 1")
	}
	if err := (func() ReverbConfig { c := valid; c.WetLevel = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative wet level")
	}
	if err := (func() ReverbConfig { c := valid; c.DryLevel = -1; return c }()).Validate(); err == nil {
		t.Fatal("want error for negative dry level")
	}
}

func TestMixerConfigValidate(t *testing.T) {
	t.Parallel()
	valid := MixerConfig{Weights: [][]float64{{1, 1}}, Normalize: true}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := (MixerConfig{}).Validate(); err == nil {
		t.Fatal("want error for no outputs")
	}
	if err := (MixerConfig{Weights: [][]float64{{}}}).Validate(); err == nil {
		t.Fatal("want error for no inputs")
	}
	if err := (func() MixerConfig {
		c := valid
		c.Weights = [][]float64{{1, 1}, {1}}
		return c
	}()).Validate(); err == nil {
		t.Fatal("want error for mismatched weight row lengths")
	}
	if err := (func() MixerConfig {
		c := valid
		c.Weights = [][]float64{{1, math.NaN()}}
		return c
	}()).Validate(); err == nil {
		t.Fatal("want error for non-finite weight")
	}
	if err := (func() MixerConfig {
		row := make([]float64, MaxMixerPorts+1)
		return MixerConfig{Weights: [][]float64{row}}
	}()).Validate(); err == nil {
		t.Fatal("want error for too many inputs")
	}
	if err := (func() MixerConfig {
		rows := make([][]float64, MaxMixerPorts+1)
		for i := range rows {
			rows[i] = []float64{1}
		}
		return MixerConfig{Weights: rows}
	}()).Validate(); err == nil {
		t.Fatal("want error for too many outputs")
	}
}
