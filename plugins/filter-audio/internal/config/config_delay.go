package config

import "fmt"

type DelayConfig struct {
	DelayMs  float64 `name:"delay-ms" help:"Delay time in milliseconds"`
	Feedback float64 `name:"feedback" help:"Fraction of the delayed signal fed back in, producing repeating echoes (0 for a single repeat)"`
	WetLevel float64 `name:"wet-level" help:"Delayed signal level"`
	DryLevel float64 `name:"dry-level" help:"Unprocessed signal level"`
}

var DefaultDelayConfig = DelayConfig{DelayMs: 300, Feedback: 0.3, WetLevel: 0.5, DryLevel: 1}

func (c DelayConfig) Validate() error {
	if !finite(c.DelayMs) || c.DelayMs <= 0 {
		return fmt.Errorf("delay time must be finite and positive")
	}
	if !finite(c.Feedback) || c.Feedback < 0 || c.Feedback >= 1 {
		return fmt.Errorf("delay feedback must be within [0, 1)")
	}
	if !finite(c.WetLevel) || c.WetLevel < 0 {
		return fmt.Errorf("delay wet level must be finite and non-negative")
	}
	if !finite(c.DryLevel) || c.DryLevel < 0 {
		return fmt.Errorf("delay dry level must be finite and non-negative")
	}
	return nil
}
