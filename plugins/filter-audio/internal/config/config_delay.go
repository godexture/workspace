package config

import (
	"fmt"
)

type DelayConfig struct {
	DelayMs  float64 `name:"delay-ms" check:"positive,finite" help:"Delay time in milliseconds"`
	Feedback float64 `name:"feedback" check:"nonnegative,finite" help:"Fraction of the delayed signal fed back in, producing repeating echoes (0 for a single repeat)"`
	WetLevel float64 `name:"wet-level" check:"nonnegative,finite" help:"Delayed signal level"`
	DryLevel float64 `name:"dry-level" check:"nonnegative,finite" help:"Unprocessed signal level"`
}

var DefaultDelayConfig = DelayConfig{DelayMs: 300, Feedback: 0.3, WetLevel: 0.5, DryLevel: 1}

func (c DelayConfig) Validate() error {
	if c.Feedback >= 1 {
		return fmt.Errorf("delay feedback must be within [0, 1)")
	}
	return nil
}
