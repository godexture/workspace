package equalizer

import (
	"fmt"
	"math"
	"sort"

	"github.com/godexture/filter-audio/internal/config"
)

type bandSpec struct {
	eqType config.EqualizerType
	freq   float64
	gainDB float64
	q      float64
}

type pair struct {
	freq float64
	gain float64
}

type Axis struct {
	Frequency float64
	GainIndex int
}

func resolveBands(cfg config.EqualizerConfig) ([]bandSpec, error) {
	if cfg.EqualizerMode == config.EqualizerModeSingle {
		return []bandSpec{{eqType: cfg.Type, freq: cfg.FrequencyHz, gainDB: cfg.GainDB, q: cfg.Q}}, nil
	}
	axis, err := ResolveAxis(cfg)
	if err != nil {
		return nil, err
	}
	gains, err := config.ParseBandList(cfg.Gains)
	if err != nil {
		return nil, err
	}
	if len(gains) != len(axis) {
		return nil, fmt.Errorf("equalizer gains has %d entries, want %d", len(gains), len(axis))
	}
	pairs := make([]pair, len(axis))
	for index, band := range axis {
		pairs[index] = pair{freq: band.Frequency, gain: gains[band.GainIndex]}
	}

	bands := make([]bandSpec, len(pairs))
	for index, pair := range pairs {
		bands[index] = bandSpec{eqType: config.EqualizerTypePeaking, freq: pair.freq, gainDB: pair.gain, q: bandQ(pairs, index)}
	}
	return bands, nil
}

func ResolveAxis(cfg config.EqualizerConfig) ([]Axis, error) {
	manual, err := config.ParseBandList(cfg.ManualBands)
	if err != nil {
		return nil, err
	}
	axis := make([]Axis, 0)
	if len(manual) > 0 {
		axis = make([]Axis, len(manual))
		for index, frequency := range manual {
			if math.IsNaN(frequency) || math.IsInf(frequency, 0) || frequency <= 0 {
				return nil, fmt.Errorf("equalizer manual-bands frequencies must be finite and positive")
			}
			axis[index] = Axis{Frequency: frequency, GainIndex: index}
		}
	} else {
		if cfg.Bands <= 0 {
			return nil, fmt.Errorf("equalizer bands must be positive")
		}
		if math.IsNaN(cfg.LowHz) || math.IsInf(cfg.LowHz, 0) || cfg.LowHz <= 0 {
			return nil, fmt.Errorf("equalizer low-hz must be finite and positive")
		}
		if math.IsNaN(cfg.HighHz) || math.IsInf(cfg.HighHz, 0) || !(cfg.HighHz > cfg.LowHz) {
			return nil, fmt.Errorf("equalizer high-hz must be finite and greater than low-hz")
		}
		ratio := math.Pow(cfg.HighHz/cfg.LowHz, 1/float64(cfg.Bands))
		axis = make([]Axis, cfg.Bands)
		for index := range axis {
			axis[index] = Axis{Frequency: cfg.LowHz * math.Pow(ratio, float64(index)+0.5), GainIndex: index}
		}
	}
	sort.Slice(axis, func(i, j int) bool { return axis[i].Frequency < axis[j].Frequency })
	return axis, nil
}

func bandQ(pairs []pair, index int) float64 {
	frequency := pairs[index].freq
	var lowEdge, highEdge float64
	switch {
	case len(pairs) == 1:
		lowEdge, highEdge = frequency/math.Sqrt2, frequency*math.Sqrt2
	case index == 0:
		highEdge = math.Sqrt(frequency * pairs[index+1].freq)
		lowEdge = frequency * frequency / highEdge
	case index == len(pairs)-1:
		lowEdge = math.Sqrt(pairs[index-1].freq * frequency)
		highEdge = frequency * frequency / lowEdge
	default:
		lowEdge = math.Sqrt(pairs[index-1].freq * frequency)
		highEdge = math.Sqrt(frequency * pairs[index+1].freq)
	}
	bandwidth := math.Log2(highEdge / lowEdge)
	p := math.Pow(2, bandwidth)
	if p <= 1 {
		return math.Sqrt2
	}
	return math.Sqrt(p) / (p - 1)
}
