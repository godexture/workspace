package config

import "fmt"

type ResampleConfig struct {
	SampleRate int `name:"sample-rate" help:"Target sample rate"`
}

var DefaultResampleConfig = ResampleConfig{}

func (c ResampleConfig) Validate() error {
	if c.SampleRate <= 0 {
		return fmt.Errorf("sample rate must be positive: %d", c.SampleRate)
	}
	return nil
}
