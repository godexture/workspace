package config

import "fmt"

type SpeedMode string

const (
	SpeedModeInterpolate SpeedMode = "interpolate"
	SpeedModeRelabel     SpeedMode = "relabel"
)

type SpeedConfig struct {
	Factor float64   `name:"factor" help:"Playback speed multiplier (e.g. 2 for double speed, 0.5 for half); pitch shifts with speed"`
	Mode   SpeedMode `name:"mode" help:"How speed is applied: interpolate (resample, keeps input sample rate) or relabel (no resampling, retags the sample rate; lossless)"`
}

var DefaultSpeedConfig = SpeedConfig{Factor: 1, Mode: SpeedModeInterpolate}

func (c SpeedConfig) Validate() error {
	if !finite(c.Factor) || c.Factor <= 0 {
		return fmt.Errorf("speed factor must be finite and positive")
	}
	if !c.Mode.Valid() {
		return fmt.Errorf("invalid speed mode: %q", c.Mode)
	}
	return nil
}
