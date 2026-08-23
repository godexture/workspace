package audio

import (
	"time"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/plugin"
)

type compressorConfig struct {
	Threshold  config.DecibelValue
	Ratio      config.RatioValue
	Attack     time.Duration
	Release    time.Duration
	Knee       config.DecibelValue
	MakeupGain config.DecibelValue
	MaxSamples int
}

func compressorSchema() config.Schema[compressorConfig] {
	return config.Struct[compressorConfigID](func() compressorConfig {
		return compressorConfig{
			Threshold:  -18,
			Ratio:      4,
			Attack:     10 * time.Millisecond,
			Release:    100 * time.Millisecond,
			Knee:       6,
			MaxSamples: defaultFilterSamples,
		}
	}).
		Version("1").
		AddField(config.Field("threshold", func(value *compressorConfig) *config.DecibelValue { return &value.Threshold },
			config.Decibel().Range(silenceFloorDB, 0).Help("level above which compression begins"))).
		AddField(config.Field("ratio", func(value *compressorConfig) *config.RatioValue { return &value.Ratio },
			config.Ratio().Range(1, 1000).Help("how much of every decibel above the threshold survives, inverted: 4 means 4:1"))).
		AddField(config.Field("attack", func(value *compressorConfig) *time.Duration { return &value.Attack },
			config.Duration().Range(0, time.Second).Help("time to react to a level increase"))).
		AddField(config.Field("release", func(value *compressorConfig) *time.Duration { return &value.Release },
			config.Duration().Range(0, time.Minute).Help("time to recover after a level drop"))).
		AddField(config.Field("knee", func(value *compressorConfig) *config.DecibelValue { return &value.Knee },
			config.Decibel().Range(0, 60).Help("width of the soft knee around the threshold; zero is a hard corner"))).
		AddField(config.Field("makeupGain", func(value *compressorConfig) *config.DecibelValue { return &value.MakeupGain },
			config.Decibel().Range(-60, 60).Help("level applied after compression to restore what it took"))).
		AddField(budget(func(value *compressorConfig) *int { return &value.MaxSamples })).
		Build()
}

func newCompressor() plugin.Component {
	return newFilter[compressorID](filterSpec[compressorConfig]{
		name:    "Compressor",
		detail:  "audio.compressor",
		schema:  compressorSchema(),
		samples: func(value *compressorConfig) *int { return &value.MaxSamples },
		build: func(value compressorConfig, signal filterStream) (filter, error) {
			return &compressor{
				threshold: float64(value.Threshold),
				ratio:     float64(value.Ratio),
				knee:      float64(value.Knee),
				makeup:    amplitude(float64(value.MakeupGain)),
				attack:    timeConstant(value.Attack, signal.Rate),
				release:   timeConstant(value.Release, signal.Rate),
			}, nil
		},
	})
}

type compressor struct {
	threshold, ratio, knee float64
	makeup                 float32
	attack, release        float32
	envelope               float32
}

func (c *compressor) Apply(planes [][]float32) {
	if len(planes) == 0 {
		return
	}
	for index := range planes[0] {
		target := float32(reduction(decibels(linkedPeak(planes, index)), c.threshold, c.ratio, c.knee))
		coefficient := c.release
		if target < c.envelope {
			coefficient = c.attack
		}
		c.envelope = coefficient*c.envelope + (1-coefficient)*target
		gain := amplitude(float64(c.envelope)) * c.makeup
		for _, samples := range planes {
			samples[index] *= gain
		}
	}
}

// reduction is the feed-forward soft-knee static curve (Giannoulis et al.):
// how many decibels to remove at this level, always at most zero.
func reduction(level, threshold, ratio, knee float64) float64 {
	overshoot := level - threshold
	half := knee / 2
	switch {
	case knee <= 0:
		if overshoot <= 0 {
			return 0
		}
		return overshoot * (1/ratio - 1)
	case overshoot <= -half:
		return 0
	case overshoot >= half:
		return overshoot * (1/ratio - 1)
	default:
		shifted := overshoot + half
		return (1/ratio - 1) * shifted * shifted / (2 * knee)
	}
}
