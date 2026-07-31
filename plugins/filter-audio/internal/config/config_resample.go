package config

type ResampleConfig struct {
	SampleRate int `name:"sample-rate" check:"positive" help:"Target sample rate"`
}

var DefaultResampleConfig = ResampleConfig{}

func (c ResampleConfig) Validate() error {
	return nil
}
