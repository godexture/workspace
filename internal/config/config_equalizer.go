package config

import (
	"fmt"
	"strconv"
	"strings"
)

type EqualizerType string

const (
	EqualizerTypePeaking   EqualizerType = "peaking"
	EqualizerTypeLowShelf  EqualizerType = "lowshelf"
	EqualizerTypeHighShelf EqualizerType = "highshelf"
	EqualizerTypeLowPass   EqualizerType = "lowpass"
	EqualizerTypeHighPass  EqualizerType = "highpass"
)

type EqualizerMode string

const (
	EqualizerModeSingle    EqualizerMode = "single"
	EqualizerModeMultiband EqualizerMode = "multiband"
)

type EqualizerConfig struct {
	EqualizerMode EqualizerMode `name:"mode" help:"single (one parametric/shelf/pass band) or multiband (cascaded peaking bands across a frequency range)"`

	Type        EqualizerType `name:"type" depends-on:"mode=single" help:"Filter shape: peaking, lowshelf, highshelf, lowpass, or highpass"`
	FrequencyHz float64       `name:"frequency-hz" depends-on:"mode=single" check:"positive,finite" help:"Center frequency (peaking/shelf) or corner frequency (lowpass/highpass)"`
	GainDB      float64       `name:"gain-db" depends-on:"mode=single" check:"finite" help:"Gain applied at the center frequency; ignored by lowpass/highpass"`
	Q           float64       `name:"q" depends-on:"mode=single" check:"positive,finite" help:"Filter Q; higher values narrow the band or sharpen the corner"`

	Bands       int     `name:"bands" depends-on:"mode=multiband" check:"positive" help:"Band count for automatic log-spaced split across [low-hz, high-hz]; ignored if manual-bands is set"`
	LowHz       float64 `name:"low-hz" depends-on:"mode=multiband" check:"positive,finite" help:"Lower bound for automatic band split"`
	HighHz      float64 `name:"high-hz" depends-on:"mode=multiband" check:"positive,finite" help:"Upper bound for automatic band split"`
	ManualBands string  `name:"manual-bands" depends-on:"mode=multiband" help:"Comma-separated explicit band center frequencies in Hz; overrides bands/low-hz/high-hz"`
	Gains       string  `name:"gains" editor:"sliders" depends-on:"mode=multiband" help:"Comma-separated per-band gain in dB; length must match the resolved band count"`
}

var DefaultEqualizerConfig = EqualizerConfig{
	EqualizerMode: EqualizerModeSingle,
	Type:          EqualizerTypePeaking,
	FrequencyHz:   1000,
	Q:             0.7071067811865476,
	Bands:         10,
	LowHz:         20,
	HighHz:        20000,
	Gains:         "0,0,0,0,0,0,0,0,0,0",
}

func (c EqualizerConfig) Validate() error {
	if !c.EqualizerMode.Valid() {
		return fmt.Errorf("invalid equalizer mode: %q", c.EqualizerMode)
	}
	if c.EqualizerMode == EqualizerModeSingle {
		if !c.Type.Valid() {
			return fmt.Errorf("invalid equalizer type: %q", c.Type)
		}
		return nil
	}
	manual, err := ParseBandList(c.ManualBands)
	if err != nil {
		return fmt.Errorf("invalid manual-bands: %w", err)
	}
	gains, err := ParseBandList(c.Gains)
	if err != nil {
		return fmt.Errorf("invalid gains: %w", err)
	}
	resolvedCount := c.Bands
	if len(manual) > 0 {
		resolvedCount = len(manual)
	} else if !(c.HighHz > c.LowHz) {
		return fmt.Errorf("equalizer high-hz must be greater than low-hz")
	}
	if len(gains) != resolvedCount {
		return fmt.Errorf("equalizer gains has %d entries, want %d (matching the resolved band count)", len(gains), resolvedCount)
	}
	for _, frequency := range manual {
		if !finite(frequency) || frequency <= 0 {
			return fmt.Errorf("equalizer manual-bands frequencies must be finite and positive")
		}
	}
	for _, gain := range gains {
		if !finite(gain) {
			return fmt.Errorf("equalizer gains must be finite")
		}
	}
	return nil
}

func ParseBandList(raw string) ([]float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	result := make([]float64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid value %q: %w", part, err)
		}
		result = append(result, value)
	}
	return result, nil
}
