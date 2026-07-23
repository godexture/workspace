package config

import "fmt"

type GateMode string

const (
	// GateModeHard instantly silences every sample below the threshold.
	GateModeHard GateMode = "hard"
	// GateModeLowpass is a Buchla-style low-pass gate: as the level falls
	// below the threshold, a one-pole low-pass filter and the output level
	// close together (envelope-controlled), so the sound darkens and fades
	// out smoothly instead of being cut off abruptly.
	GateModeLowpass GateMode = "lowpass"
)

type GateConfig struct {
	ThresholdDBFS    float64  `name:"threshold-dbfs" help:"Level below which the gate begins to close"`
	GateMode         GateMode `name:"mode" help:"How the gate responds: hard (instantly silences samples below the threshold) or lowpass (Buchla-style low-pass gate that darkens and fades out smoothly)"`
	RangeDB          float64  `name:"range-db" help:"dB below the threshold over which the gate fully closes (lowpass mode only)"`
	AttackMs         float64  `name:"attack-ms" help:"Time for the gate to open, in milliseconds (lowpass mode only)"`
	ReleaseMs        float64  `name:"release-ms" help:"Time for the gate to close, in milliseconds (lowpass mode only)"`
	OpenFrequencyHz  float64  `name:"open-frequency-hz" help:"Low-pass cutoff when the gate is fully open (lowpass mode only)"`
	CloseFrequencyHz float64  `name:"close-frequency-hz" help:"Low-pass cutoff when the gate is fully closed (lowpass mode only)"`
}

var DefaultGateConfig = GateConfig{ThresholdDBFS: -60, GateMode: GateModeHard, RangeDB: 40, AttackMs: 5, ReleaseMs: 50, OpenFrequencyHz: 20000, CloseFrequencyHz: 200}

func (c GateConfig) Validate() error {
	if !finite(c.ThresholdDBFS) || c.ThresholdDBFS > 0 {
		return fmt.Errorf("gate threshold must be finite and no greater than 0 dBFS")
	}
	if !c.GateMode.Valid() {
		return fmt.Errorf("invalid gate mode: %q", c.GateMode)
	}
	if c.GateMode != GateModeLowpass {
		return nil
	}
	if !finite(c.RangeDB) || c.RangeDB < 0 {
		return fmt.Errorf("gate range must be finite and non-negative")
	}
	if c.AttackMs < 0 || c.ReleaseMs < 0 {
		return fmt.Errorf("gate attack and release must not be negative")
	}
	if !finite(c.OpenFrequencyHz) || c.OpenFrequencyHz <= 0 || !finite(c.CloseFrequencyHz) || c.CloseFrequencyHz <= 0 {
		return fmt.Errorf("gate frequencies must be finite and positive")
	}
	if c.CloseFrequencyHz > c.OpenFrequencyHz {
		return fmt.Errorf("gate close frequency must not exceed open frequency")
	}
	return nil
}
