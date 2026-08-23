package audio

import (
	"fmt"
	"math"
	"slices"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plugin"
)

// bandType is the shape of one band's response.
type bandType string

const (
	peakingBand   bandType = "peaking"
	lowShelfBand  bandType = "lowshelf"
	highShelfBand bandType = "highshelf"
	lowPassBand   bandType = "lowpass"
	highPassBand  bandType = "highpass"
)

// equalizerBand is one section of the cascade. A zero Q is a request rather
// than an omission: derive a width from where the neighbouring bands sit,
// which is what makes a row of bands behave like one equalizer instead of a
// set of unrelated filters.
type equalizerBand struct {
	Type      bandType
	Frequency config.FrequencyValue
	Gain      config.DecibelValue
	Q         config.RatioValue
}

func bandSchema() config.Schema[equalizerBand] {
	return config.Struct[equalizerBandID](func() equalizerBand {
		return equalizerBand{Type: peakingBand, Frequency: 1_000}
	}).
		Version("1").
		AddField(config.Field("type", func(value *equalizerBand) *bandType { return &value.Type },
			config.Enum(
				config.Choice[bandType]{ID: string(peakingBand), Label: "Peaking", Value: peakingBand},
				config.Choice[bandType]{ID: string(lowShelfBand), Label: "Low shelf", Value: lowShelfBand},
				config.Choice[bandType]{ID: string(highShelfBand), Label: "High shelf", Value: highShelfBand},
				config.Choice[bandType]{ID: string(lowPassBand), Label: "Low-pass", Value: lowPassBand},
				config.Choice[bandType]{ID: string(highPassBand), Label: "High-pass", Value: highPassBand},
			))).
		AddField(config.Field("frequency", func(value *equalizerBand) *config.FrequencyValue { return &value.Frequency },
			config.Frequency().Range(1, 768_000).Help("centre of a peak, or corner of a shelf or a pass"))).
		AddField(config.Field("gain", func(value *equalizerBand) *config.DecibelValue { return &value.Gain },
			config.Decibel().Range(-60, 60).Help("level change at the band, which the pass types ignore"))).
		AddField(config.Field("q", func(value *equalizerBand) *config.RatioValue { return &value.Q },
			config.Ratio().Range(0, 100).Help("width of the band, or zero to derive one from the neighbouring bands"))).
		Build()
}

type equalizerConfig struct {
	Bands      []equalizerBand
	MaxSamples int
}

func equalizerSchema() config.Schema[equalizerConfig] {
	return config.Struct[equalizerConfigID](func() equalizerConfig {
		return equalizerConfig{MaxSamples: defaultFilterSamples}
	}).
		Version("1").
		AddField(config.Field("bands", func(value *equalizerConfig) *[]equalizerBand { return &value.Bands },
			config.Slice(config.Nested(bandSchema())).Help("the cascade, applied in ascending frequency"))).
		AddField(budget(func(value *equalizerConfig) *int { return &value.MaxSamples })).
		Build()
}

func newEqualizer() plugin.Component {
	return newFilter[equalizerID](filterSpec[equalizerConfig]{
		name:    "Equalizer",
		detail:  "audio.equalizer",
		schema:  equalizerSchema(),
		samples: func(value *equalizerConfig) *int { return &value.MaxSamples },
		check: func(value equalizerConfig, signal sample.Signal) error {
			for _, band := range value.Bands {
				if float64(band.Frequency) >= float64(signal.Rate)/2 {
					return fmt.Errorf("%w: a band at %d Hz is at or above half of this stream's %d Hz",
						ErrUnsupported, band.Frequency, signal.Rate)
				}
			}
			return nil
		},
		build: func(value equalizerConfig, signal sample.Signal) (filter, error) {
			return newEqualizerKernel(value.Bands, signal), nil
		},
	})
}

type equalizer struct {
	sections []biquad
	// state is one running section per band per channel. The cascade runs in
	// ascending frequency, which is the order the derived widths are read in;
	// the sections are linear, so the order changes nothing else.
	state [][]biquadState
}

func newEqualizerKernel(bands []equalizerBand, signal sample.Signal) *equalizer {
	ordered := append([]equalizerBand(nil), bands...)
	slices.SortStableFunc(ordered, func(left, right equalizerBand) int {
		return int(left.Frequency) - int(right.Frequency)
	})
	frequencies := make([]float64, len(ordered))
	for index, band := range ordered {
		frequencies[index] = float64(band.Frequency)
	}
	result := &equalizer{}
	channels := signal.Layout.Count()
	for index, band := range ordered {
		// A peak or a shelf asked for no level change is not a filter, and
		// running one anyway would still round every sample through five
		// coefficients. The band stays in the frequency list above, because a
		// slider left at zero is still part of the axis its neighbours derive
		// their widths from.
		if band.Gain == 0 && band.Type != lowPassBand && band.Type != highPassBand {
			continue
		}
		width := float64(band.Q)
		if width == 0 {
			width = derivedQ(frequencies, index)
		}
		result.sections = append(result.sections, newBiquad(band.Type, frequencies[index], float64(band.Gain), width, signal.Rate))
		result.state = append(result.state, make([]biquadState, channels))
	}
	return result
}

func (e *equalizer) Apply(planes [][]float32) {
	for index, section := range e.sections {
		for channel, samples := range planes {
			state := &e.state[index][channel]
			for position, input := range samples {
				output := section.b0*input + section.b1*state.x1 + section.b2*state.x2 -
					section.a1*state.y1 - section.a2*state.y2
				state.x2, state.x1 = state.x1, input
				state.y2, state.y1 = state.y1, output
				samples[position] = output
			}
		}
	}
}

// derivedQ gives a band the width that makes it meet its neighbours: the
// geometric midpoint on each side becomes an edge, so a row of bands covers
// the range once rather than overlapping or leaving gaps between. A band with
// no neighbours spans an octave.
func derivedQ(frequencies []float64, index int) float64 {
	frequency := frequencies[index]
	var low, high float64
	switch {
	case len(frequencies) == 1:
		low, high = frequency/math.Sqrt2, frequency*math.Sqrt2
	case index == 0:
		high = math.Sqrt(frequency * frequencies[1])
		low = frequency * frequency / high
	case index == len(frequencies)-1:
		low = math.Sqrt(frequencies[index-1] * frequency)
		high = frequency * frequency / low
	default:
		low = math.Sqrt(frequencies[index-1] * frequency)
		high = math.Sqrt(frequency * frequencies[index+1])
	}
	span := high / low
	if span <= 1 {
		return math.Sqrt2
	}
	return math.Sqrt(span) / (span - 1)
}
