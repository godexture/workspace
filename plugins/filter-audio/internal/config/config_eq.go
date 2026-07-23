package config

import "fmt"

type EQType string

const (
	EQTypePeaking   EQType = "peaking"
	EQTypeLowShelf  EQType = "lowshelf"
	EQTypeHighShelf EQType = "highshelf"
	EQTypeLowPass   EQType = "lowpass"
	EQTypeHighPass  EQType = "highpass"
)

type EQConfig struct {
	Type        EQType  `name:"type" help:"Filter shape: peaking, lowshelf, highshelf, lowpass, or highpass"`
	FrequencyHz float64 `name:"frequency-hz" help:"Center frequency (peaking/shelf) or corner frequency (lowpass/highpass)"`
	GainDB      float64 `name:"gain-db" help:"Gain applied at the center frequency; ignored by lowpass/highpass"`
	Q           float64 `name:"q" help:"Filter Q; higher values narrow the band or sharpen the corner"`
}

var DefaultEQConfig = EQConfig{Type: EQTypePeaking, FrequencyHz: 1000, Q: 0.7071067811865476}

func (c EQConfig) Validate() error {
	if !c.Type.Valid() {
		return fmt.Errorf("invalid eq type: %q", c.Type)
	}
	if !finite(c.FrequencyHz) || c.FrequencyHz <= 0 {
		return fmt.Errorf("eq frequency must be finite and positive")
	}
	if !finite(c.Q) || c.Q <= 0 {
		return fmt.Errorf("eq Q must be finite and positive")
	}
	if !finite(c.GainDB) {
		return fmt.Errorf("eq gain must be finite")
	}
	return nil
}
