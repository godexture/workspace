package config

import (
	"fmt"
)

type ReverbConfig struct {
	RoomSize float64 `name:"room-size" check:"nonnegative,finite" help:"Room size / decay time (0-1)"`
	Damping  float64 `name:"damping" check:"nonnegative,finite" help:"High-frequency damping applied to the reverb tail (0-1)"`
	WetLevel float64 `name:"wet-level" check:"nonnegative,finite" help:"Reverberated signal level"`
	DryLevel float64 `name:"dry-level" check:"nonnegative,finite" help:"Unprocessed signal level"`
}

var DefaultReverbConfig = ReverbConfig{RoomSize: 0.5, Damping: 0.5, WetLevel: 0.3, DryLevel: 1}

func (c ReverbConfig) Validate() error {
	if c.RoomSize > 1 {
		return fmt.Errorf("reverb room size must be within [0, 1]")
	}
	if c.Damping > 1 {
		return fmt.Errorf("reverb damping must be within [0, 1]")
	}
	return nil
}
