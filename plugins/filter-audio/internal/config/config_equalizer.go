package config

import "fmt"

type EqualizerType string

const (
	EqualizerTypePeaking   EqualizerType = "peaking"
	EqualizerTypeLowShelf  EqualizerType = "lowshelf"
	EqualizerTypeHighShelf EqualizerType = "highshelf"
	EqualizerTypeLowPass   EqualizerType = "lowpass"
	EqualizerTypeHighPass  EqualizerType = "highpass"
)

type EqualizerConfig struct {
	Type        EqualizerType  `name:"type" help:"Filter shape: peaking, lowshelf, highshelf, lowpass, or highpass"`
	FrequencyHz float64 `name:"frequency-hz" help:"Center frequency (peaking/shelf) or corner frequency (lowpass/highpass)"`
	GainDB      float64 `name:"gain-db" help:"Gain applied at the center frequency; ignored by lowpass/highpass"`
	Q           float64 `name:"q" help:"Filter Q; higher values narrow the band or sharpen the corner"`
}

var DefaultEqualizerConfig = EqualizerConfig{Type: EqualizerTypePeaking, FrequencyHz: 1000, Q: 0.7071067811865476}

func (c EqualizerConfig) Validate() error {
	if !c.Type.Valid() {
		return fmt.Errorf("invalid equalizer type: %q", c.Type)
	}
	if !finite(c.FrequencyHz) || c.FrequencyHz <= 0 {
		return fmt.Errorf("equalizer frequency must be finite and positive")
	}
	if !finite(c.Q) || c.Q <= 0 {
		return fmt.Errorf("equalizer Q must be finite and positive")
	}
	if !finite(c.GainDB) {
		return fmt.Errorf("equalizer gain must be finite")
	}
	return nil
}
