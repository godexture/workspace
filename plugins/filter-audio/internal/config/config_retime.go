package config

import (
	"fmt"
)

type SpeedMode string

const (
	SpeedModeInterpolate SpeedMode = "interpolate"
	SpeedModeRelabel     SpeedMode = "relabel"
)

type SpeedConfig struct {
	Factor float64   `name:"factor" check:"positive,finite" help:"Playback retime multiplier (e.g. 2 for double retime, 0.5 for half); pitch shifts with retime"`
	Mode   SpeedMode `name:"mode" help:"How retime is applied: interpolate (resample, keeps input sample rate) or relabel (no resampling, retags the sample rate; lossless)"`
}

var DefaultSpeedConfig = SpeedConfig{Factor: 1, Mode: SpeedModeInterpolate}

func (c SpeedConfig) Validate() error {
	if !c.Mode.Valid() {
		return fmt.Errorf("invalid retime mode: %q", c.Mode)
	}
	return nil
}
