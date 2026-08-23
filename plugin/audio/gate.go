package audio

import (
	"math"
	"time"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/plugin"
)

// gateMode selects what closing sounds like.
type gateMode string

const (
	// hardGate silences every sample below the threshold at once.
	hardGating gateMode = "hard"
	// lowpassGating is the Buchla-style low-pass gate: one envelope drives
	// both a one-pole cutoff and the output level, so the sound darkens as it
	// fades instead of stopping.
	lowpassGating gateMode = "lowpass"
)

type gateConfig struct {
	Threshold config.DecibelValue
	Mode      gateMode
	Range     config.DecibelValue
	Attack    time.Duration
	Release   time.Duration
	Open      config.FrequencyValue
	Close     config.FrequencyValue

	MaxSamples int
}

func gateSchema() config.Schema[gateConfig] {
	return config.Struct[gateConfigID](func() gateConfig {
		return gateConfig{
			Threshold:  -60,
			Mode:       hardGating,
			Range:      40,
			Attack:     5 * time.Millisecond,
			Release:    50 * time.Millisecond,
			Open:       20_000,
			Close:      200,
			MaxSamples: defaultFilterSamples,
		}
	}).
		Version("1").
		AddField(config.Field("threshold", func(value *gateConfig) *config.DecibelValue { return &value.Threshold },
			config.Decibel().Range(silenceFloorDB, 0).Help("level below which the gate begins to close"))).
		AddField(config.Field("mode", func(value *gateConfig) *gateMode { return &value.Mode },
			config.Enum(
				config.Choice[gateMode]{ID: string(hardGating), Label: "Hard", Value: hardGating},
				config.Choice[gateMode]{ID: string(lowpassGating), Label: "Low-pass", Value: lowpassGating},
			).Help("how the gate closes"))).
		AddField(config.Field("range", func(value *gateConfig) *config.DecibelValue { return &value.Range },
			config.Decibel().Range(0, 120).Help("decibels below the threshold over which the gate closes fully"),
			config.DependsOn("mode"))).
		AddField(config.Field("attack", func(value *gateConfig) *time.Duration { return &value.Attack },
			config.Duration().Range(0, time.Second).Help("time for the gate to open"), config.DependsOn("mode"))).
		AddField(config.Field("release", func(value *gateConfig) *time.Duration { return &value.Release },
			config.Duration().Range(0, time.Minute).Help("time for the gate to close"), config.DependsOn("mode"))).
		AddField(config.Field("openFrequency", func(value *gateConfig) *config.FrequencyValue { return &value.Open },
			config.Frequency().Range(1, 768_000).Help("low-pass corner while the gate is fully open"), config.DependsOn("mode"))).
		AddField(config.Field("closeFrequency", func(value *gateConfig) *config.FrequencyValue { return &value.Close },
			config.Frequency().Range(1, 768_000).Help("low-pass corner once the gate is fully closed"), config.DependsOn("mode"))).
		AddField(budget(func(value *gateConfig) *int { return &value.MaxSamples })).
		Validate(func(value gateConfig) []diagnostic.Item {
			if value.Mode == lowpassGating && value.Close > value.Open {
				return []diagnostic.Item{diagnostic.NewItem("audio.gate-corners", diagnostic.ErrorSeverity,
					diagnostic.Path{Fields: []string{"closeFrequency"}},
					"a gate cannot close onto a corner above the one it opens to", nil)}
			}
			return nil
		}).
		Build()
}

func newGate() plugin.Component {
	return newFilter[gateID](filterSpec[gateConfig]{
		name:    "Gate",
		detail:  "audio.gate",
		schema:  gateSchema(),
		samples: func(value *gateConfig) *int { return &value.MaxSamples },
		build: func(value gateConfig, signal filterStream) (filter, error) {
			if value.Mode == lowpassGating {
				return &lowpassGate{
					threshold: float64(value.Threshold),
					span:      float64(value.Range),
					attack:    timeConstant(value.Attack, signal.Rate),
					release:   timeConstant(value.Release, signal.Rate),
					open:      float64(value.Open),
					close:     float64(value.Close),
					rate:      signal.Rate,
					state:     make([]float32, signal.Layout.Count()),
				}, nil
			}
			return &hardGate{threshold: amplitude(float64(value.Threshold))}, nil
		},
	})
}

// hardGate silences a position only when every channel is below the threshold
// there, so multi-channel audio never loses one side and keeps the other.
type hardGate struct{ threshold float32 }

func (g *hardGate) Apply(planes [][]float32) {
	if len(planes) == 0 {
		return
	}
	for index := range planes[0] {
		if linkedPeak(planes, index) >= g.threshold {
			continue
		}
		for _, samples := range planes {
			samples[index] = 0
		}
	}
}

type lowpassGate struct {
	threshold, span float64
	attack, release float32
	open, close     float64
	rate            int
	envelope        float32
	state           []float32
}

func (g *lowpassGate) Apply(planes [][]float32) {
	if len(planes) == 0 {
		return
	}
	for index := range planes[0] {
		target := openness(decibels(linkedPeak(planes, index)), g.threshold, g.span)
		coefficient := g.release
		if target > g.envelope {
			coefficient = g.attack
		}
		g.envelope = coefficient*g.envelope + (1-coefficient)*target
		// The corner sweeps exponentially between the two, which is how pitch
		// is heard, so the tail darkens evenly rather than all at the end.
		cutoff := g.close * math.Pow(g.open/g.close, float64(g.envelope))
		coefficient = float32(1 - math.Exp(-2*math.Pi*cutoff/float64(g.rate)))
		for channel, samples := range planes {
			filtered := g.state[channel] + coefficient*(samples[index]-g.state[channel])
			g.state[channel] = filtered
			samples[index] = filtered * g.envelope
		}
	}
}

// openness is one at or above the threshold, zero once the level has fallen
// the whole range below it, and linear in decibels between.
func openness(level, threshold, span float64) float32 {
	if span <= 0 {
		if level >= threshold {
			return 1
		}
		return 0
	}
	return float32(min(max((level-(threshold-span))/span, 0), 1))
}
