package config

import "fmt"

type GainConfig struct {
	Decibels float64 `name:"decibels" help:"Gain in dB"`
}

var DefaultGainConfig = GainConfig{}

func (c GainConfig) Validate() error {
	if !finite(c.Decibels) {
		return fmt.Errorf("gain must be finite")
	}
	return nil
}
