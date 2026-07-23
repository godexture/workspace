package config

import "fmt"

type DCOffsetConfig struct {
	Pole float64 `name:"pole" help:"DC offset filter pole"`
}

var DefaultDCOffsetConfig = DCOffsetConfig{Pole: 0.995}

func (c DCOffsetConfig) Validate() error {
	if !finite(c.Pole) || c.Pole <= 0 || c.Pole >= 1 {
		return fmt.Errorf("DC offset pole must be in (0, 1)")
	}
	return nil
}
