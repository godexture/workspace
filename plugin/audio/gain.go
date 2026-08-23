package audio

import (
	"math"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plugin"
)

type gainConfig struct {
	Decibels   config.DecibelValue
	MaxSamples int
}

func gainSchema() config.Schema[gainConfig] {
	return config.Struct[gainConfigID](func() gainConfig { return gainConfig{MaxSamples: defaultFilterSamples} }).
		Version("1").
		AddField(config.Field("decibels", func(value *gainConfig) *config.DecibelValue { return &value.Decibels },
			config.Decibel().Help("level change applied to every sample"))).
		AddField(budget(func(value *gainConfig) *int { return &value.MaxSamples })).
		Build()
}

func newGain() plugin.Component {
	return newFilter[gainID]("Gain", "audio.gain", gainSchema(),
		func(value *gainConfig) *int { return &value.MaxSamples },
		func(value gainConfig, _ sample.Signal) (filter, error) {
			return gain{factor: float32(math.Pow(10, float64(value.Decibels)/20))}, nil
		})
}

type gain struct{ factor float32 }

func (g gain) Apply(planes [][]float32) {
	if g.factor == 1 {
		return
	}
	for _, samples := range planes {
		for index := range samples {
			samples[index] *= g.factor
		}
	}
}
