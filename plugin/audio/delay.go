package audio

import (
	"math"
	"time"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/plugin"
)

type delayConfig struct {
	Time       time.Duration
	Feedback   config.RatioValue
	Wet        config.RatioValue
	Dry        config.RatioValue
	MaxSamples int
}

func delaySchema() config.Schema[delayConfig] {
	return config.Struct[delayConfigID](func() delayConfig {
		return delayConfig{Time: 300 * time.Millisecond, Feedback: 0.3, Wet: 0.5, Dry: 1, MaxSamples: defaultFilterSamples}
	}).
		Version("1").
		AddField(config.Field("time", func(value *delayConfig) *time.Duration { return &value.Time },
			config.Duration().Range(time.Microsecond, 10*time.Second).Help("how far behind the repeat arrives"))).
		AddField(config.Field("feedback", func(value *delayConfig) *config.RatioValue { return &value.Feedback },
			config.Ratio().Range(0, 0.999).Help("how much of the repeat is fed back in, turning one echo into a decaying series"))).
		AddField(config.Field("wet", func(value *delayConfig) *config.RatioValue { return &value.Wet },
			config.Ratio().Range(0, 4).Help("level of the delayed signal"))).
		AddField(config.Field("dry", func(value *delayConfig) *config.RatioValue { return &value.Dry },
			config.Ratio().Range(0, 4).Help("level of the signal that was not delayed"))).
		AddField(budget(func(value *delayConfig) *int { return &value.MaxSamples })).
		Build()
}

func newDelay() plugin.Component {
	return newFilter[delayID](filterSpec[delayConfig]{
		name:    "Delay",
		detail:  "audio.delay",
		schema:  delaySchema(),
		samples: func(value *delayConfig) *int { return &value.MaxSamples },
		build: func(value delayConfig, signal filterStream) (filter, error) {
			length := max(int(math.Round(value.Time.Seconds()*float64(signal.Rate))), 1)
			lines := make([][]float32, signal.Layout.Count())
			for channel := range lines {
				lines[channel] = make([]float32, length)
			}
			return &delay{
				lines:    lines,
				feedback: float32(value.Feedback),
				wet:      float32(value.Wet),
				dry:      float32(value.Dry),
			}, nil
		},
	})
}

// delay is one circular line per channel, all sharing a write position because
// every channel is delayed by the same time. What comes back out is mixed with
// what went in, and a fraction of it is written back so a single repeat
// becomes a decaying series.
type delay struct {
	lines    [][]float32
	position int
	feedback float32
	wet, dry float32
}

func (d *delay) Apply(planes [][]float32) {
	if len(planes) == 0 {
		return
	}
	length := len(d.lines[0])
	for index := range planes[0] {
		position := d.position
		for channel, samples := range planes {
			line := d.lines[channel]
			delayed := line[position]
			line[position] = samples[index] + delayed*d.feedback
			samples[index] = samples[index]*d.dry + delayed*d.wet
		}
		d.position++
		if d.position == length {
			d.position = 0
		}
	}
}
