package audio

import (
	"fmt"
	"math"
	"time"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/plugin"
)

type fadeConfig struct {
	In         time.Duration
	Out        time.Duration
	MaxSamples int
}

func fadeSchema() config.Schema[fadeConfig] {
	return config.Struct[fadeConfigID](func() fadeConfig {
		return fadeConfig{MaxSamples: defaultFilterSamples}
	}).
		Version("1").
		AddField(config.Field("in", func(value *fadeConfig) *time.Duration { return &value.In },
			config.Duration().Range(0, time.Hour).Help("how long the stream takes to rise from silence at its start"))).
		AddField(config.Field("out", func(value *fadeConfig) *time.Duration { return &value.Out },
			config.Duration().Range(0, time.Hour).Help("how long the stream takes to fall to silence at its end"))).
		AddField(budget(func(value *fadeConfig) *int { return &value.MaxSamples })).
		Build()
}

// newFade needs the end of the stream only when a fade-out was asked for, and
// only then does it insist the stream state one. Holding the stream to find
// its end would turn the cheapest processor in the family into the most
// expensive, so it asks instead, and says so at planning time when the answer
// is not there.
func newFade() plugin.Component {
	return newFilter[fadeID](filterSpec[fadeConfig]{
		name:    "Fade",
		detail:  "audio.fade",
		schema:  fadeSchema(),
		samples: func(value *fadeConfig) *int { return &value.MaxSamples },
		check: func(value fadeConfig, signal filterStream) error {
			if value.Out > 0 && !signal.Length.Valid() {
				return fmt.Errorf("%w: a fade-out needs to know where the stream ends, and this one does not say", ErrUnsupported)
			}
			return nil
		},
		build: func(value fadeConfig, signal filterStream) (filter, error) {
			return &fade{
				in:     inSamples(value.In, signal.Rate),
				out:    inSamples(value.Out, signal.Rate),
				length: signal.Length.Value().Int64(),
			}, nil
		},
	})
}

func inSamples(span time.Duration, rate int) int64 {
	return int64(math.Round(span.Seconds() * float64(rate)))
}

// fade counts the positions it has seen rather than reading their timestamps,
// because what a fade is about is how far into the stream a sample sits, and
// the count is that whether or not the frames arrived with timestamps on them.
type fade struct {
	in, out  int64
	length   int64
	position int64
}

func (f *fade) Apply(planes [][]float32) {
	if len(planes) == 0 {
		return
	}
	for index := range planes[0] {
		if gain := f.gain(f.position + int64(index)); gain != 1 {
			for _, samples := range planes {
				samples[index] *= gain
			}
		}
	}
	f.position += int64(len(planes[0]))
}

func (f *fade) gain(position int64) float32 {
	gain := float32(1)
	if f.in > 0 && position < f.in {
		gain = float32(position) / float32(f.in)
	}
	if f.out > 0 {
		remaining := f.length - position
		if remaining <= f.out {
			gain = min(gain, float32(max(remaining, 0))/float32(f.out))
		}
	}
	return gain
}
