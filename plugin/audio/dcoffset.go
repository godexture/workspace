package audio

import (
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plugin"
)

type dcOffsetConfig struct {
	Pole       config.RatioValue
	MaxSamples int
}

func dcOffsetSchema() config.Schema[dcOffsetConfig] {
	return config.Struct[dcOffsetConfigID](func() dcOffsetConfig {
		return dcOffsetConfig{Pole: 0.995, MaxSamples: defaultFilterSamples}
	}).
		Version("1").
		AddField(config.Field("pole", func(value *dcOffsetConfig) *config.RatioValue { return &value.Pole },
			config.Ratio().Range(0.001, 0.999).
				Help("how far the high-pass corner sits below the sample rate; nearer one removes less of the low end"))).
		AddField(budget(func(value *dcOffsetConfig) *int { return &value.MaxSamples })).
		Build()
}

func newDCOffset() plugin.Component {
	return newFilter[dcOffsetID](filterSpec[dcOffsetConfig]{
		name:    "DC offset removal",
		detail:  "audio.dc-offset",
		schema:  dcOffsetSchema(),
		samples: func(value *dcOffsetConfig) *int { return &value.MaxSamples },
		build: func(value dcOffsetConfig, signal sample.Signal) (filter, error) {
			channels := signal.Layout.Count()
			return &dcOffset{
				pole:  float32(value.Pole),
				lastX: make([]float32, channels),
				lastY: make([]float32, channels),
			}, nil
		},
	})
}

// dcOffset is the standard one-pole DC blocker, y[n] = x[n] - x[n-1] + p*y[n-1].
// Its state is per channel and survives between frames, which is what keeps a
// chunk boundary from stepping the output.
type dcOffset struct {
	pole  float32
	lastX []float32
	lastY []float32
}

func (d *dcOffset) Apply(planes [][]float32) {
	for channel, samples := range planes {
		lastX, lastY := d.lastX[channel], d.lastY[channel]
		for index, value := range samples {
			lastY = value - lastX + d.pole*lastY
			lastX = value
			samples[index] = lastY
		}
		d.lastX[channel], d.lastY[channel] = lastX, lastY
	}
}
