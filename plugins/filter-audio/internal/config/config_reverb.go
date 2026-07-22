package config

import "fmt"

type ReverbConfig struct {
	RoomSize float64 `name:"room-size" help:"Room size / decay time (0-1)"`
	Damping  float64 `name:"damping" help:"High-frequency damping applied to the reverb tail (0-1)"`
	WetLevel float64 `name:"wet-level" help:"Reverberated signal level"`
	DryLevel float64 `name:"dry-level" help:"Unprocessed signal level"`
}

var DefaultReverbConfig = ReverbConfig{RoomSize: 0.5, Damping: 0.5, WetLevel: 0.3, DryLevel: 1}

func (c ReverbConfig) Validate() error {
	if !finite(c.RoomSize) || c.RoomSize < 0 || c.RoomSize > 1 {
		return fmt.Errorf("reverb room size must be within [0, 1]")
	}
	if !finite(c.Damping) || c.Damping < 0 || c.Damping > 1 {
		return fmt.Errorf("reverb damping must be within [0, 1]")
	}
	if !finite(c.WetLevel) || c.WetLevel < 0 {
		return fmt.Errorf("reverb wet level must be finite and non-negative")
	}
	if !finite(c.DryLevel) || c.DryLevel < 0 {
		return fmt.Errorf("reverb dry level must be finite and non-negative")
	}
	return nil
}
