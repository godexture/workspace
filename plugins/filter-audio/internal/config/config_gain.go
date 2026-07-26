package config

type GainConfig struct {
	Decibels float64 `name:"decibels" check:"finite" help:"Gain in dB"`
}

var DefaultGainConfig = GainConfig{}

func (c GainConfig) Validate() error {
	return nil
}
