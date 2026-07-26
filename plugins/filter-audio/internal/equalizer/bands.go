package equalizer

import (
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

func resolveBands(cfg config.EqualizerConfig) ([]bandSpec, error) {
	if cfg.EqualizerMode == config.EqualizerModeSingle {
		return []bandSpec{{eqType: cfg.Type, freq: cfg.FrequencyHz, gainDB: cfg.GainDB, q: cfg.Q}}, nil
	}
	manual, err := config.ParseBandList(cfg.ManualBands)
	if err != nil {
		return nil, err
	}
	gains, err := config.ParseBandList(cfg.Gains)
	if err != nil {
		return nil, err
	}
	pairs := make([]pair, 0)
	if len(manual) > 0 {
		pairs = make([]pair, len(manual))
		for index, frequency := range manual {
			pairs[index] = pair{freq: frequency, gain: gains[index]}
		}
	} else {
		ratio := math.Pow(cfg.HighHz/cfg.LowHz, 1/float64(cfg.Bands))
		pairs = make([]pair, cfg.Bands)
		for index := range pairs {
			pairs[index] = pair{freq: cfg.LowHz * math.Pow(ratio, float64(index)+0.5), gain: gains[index]}
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].freq < pairs[j].freq })

	bands := make([]bandSpec, len(pairs))
	for index, pair := range pairs {
		bands[index] = bandSpec{eqType: config.EqualizerTypePeaking, freq: pair.freq, gainDB: pair.gain, q: bandQ(pairs, index)}
	}
	return bands, nil
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
