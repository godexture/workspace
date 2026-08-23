package audio

import (
	"math"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/media/sample"
	"github.com/godexture/godec/plugin"
)

// A Freeverb-style network: per channel, eight comb filters in parallel, each
// a delay line with a one-pole low-pass in its feedback path that gives the
// tail its damping, feeding four allpass filters in series that diffuse the
// result into something smooth rather than metallic. The tuning constants are
// Schroeder and Jezar's, scaled from the sample counts they were chosen at.
const (
	combCount     = 8
	allpassCount  = 4
	referenceRate = 44_100

	// channelSpread staggers each channel's line lengths so that a
	// multi-channel input decorrelates into a wider tail instead of every
	// channel reverberating identically.
	channelSpread = 23

	allpassFeedback = 0.5
	// combGain keeps the amplified feedback of the comb network from clipping.
	combGain = 0.015

	roomScale  = 0.28
	roomOffset = 0.7
	dampScale  = 0.4
)

var (
	combLengths    = [combCount]int{1116, 1188, 1277, 1356, 1422, 1491, 1557, 1617}
	allpassLengths = [allpassCount]int{556, 441, 341, 225}
)

type reverbConfig struct {
	Room       config.RatioValue
	Damping    config.RatioValue
	Wet        config.RatioValue
	Dry        config.RatioValue
	MaxSamples int
}

func reverbSchema() config.Schema[reverbConfig] {
	return config.Struct[reverbConfigID](func() reverbConfig {
		return reverbConfig{Room: 0.5, Damping: 0.5, Wet: 0.3, Dry: 1, MaxSamples: defaultFilterSamples}
	}).
		Version("1").
		AddField(config.Field("room", func(value *reverbConfig) *config.RatioValue { return &value.Room },
			config.Ratio().Range(0, 1).Help("size of the room, which is how long the tail takes to decay"))).
		AddField(config.Field("damping", func(value *reverbConfig) *config.RatioValue { return &value.Damping },
			config.Ratio().Range(0, 1).Help("how much of the high end the tail loses as it decays"))).
		AddField(config.Field("wet", func(value *reverbConfig) *config.RatioValue { return &value.Wet },
			config.Ratio().Range(0, 4).Help("level of the reverberated signal"))).
		AddField(config.Field("dry", func(value *reverbConfig) *config.RatioValue { return &value.Dry },
			config.Ratio().Range(0, 4).Help("level of the signal that was not reverberated"))).
		AddField(budget(func(value *reverbConfig) *int { return &value.MaxSamples })).
		Build()
}

func newReverb() plugin.Component {
	return newFilter[reverbID](filterSpec[reverbConfig]{
		name:    "Reverb",
		detail:  "audio.reverb",
		schema:  reverbSchema(),
		samples: func(value *reverbConfig) *int { return &value.MaxSamples },
		build: func(value reverbConfig, signal sample.Signal) (filter, error) {
			damping := float32(float64(value.Damping) * dampScale)
			result := &reverb{
				wet:      float32(value.Wet),
				dry:      float32(value.Dry),
				networks: make([]reverbNetwork, signal.Layout.Count()),
			}
			for channel := range result.networks {
				result.networks[channel] = newReverbNetwork(channel, signal.Rate,
					float32(roomOffset+float64(value.Room)*roomScale), damping)
			}
			return result, nil
		},
	})
}

type comb struct {
	line     []float32
	position int
	stored   float32
	feedback float32
	damp     float32
}

func (c *comb) process(input float32) float32 {
	output := c.line[c.position]
	c.stored = output*(1-c.damp) + c.stored*c.damp
	c.line[c.position] = input + c.stored*c.feedback
	c.position++
	if c.position == len(c.line) {
		c.position = 0
	}
	return output
}

type allpass struct {
	line     []float32
	position int
}

func (a *allpass) process(input float32) float32 {
	stored := a.line[a.position]
	a.line[a.position] = input + stored*allpassFeedback
	a.position++
	if a.position == len(a.line) {
		a.position = 0
	}
	return stored - input
}

type reverbNetwork struct {
	combs     [combCount]comb
	allpasses [allpassCount]allpass
}

func newReverbNetwork(channel, rate int, feedback, damping float32) reverbNetwork {
	var result reverbNetwork
	for index := range result.combs {
		result.combs[index] = comb{
			line:     make([]float32, scaledLength(combLengths[index], channel, rate)),
			feedback: feedback,
			damp:     damping,
		}
	}
	for index := range result.allpasses {
		result.allpasses[index] = allpass{line: make([]float32, scaledLength(allpassLengths[index], channel, rate))}
	}
	return result
}

func (n *reverbNetwork) process(input float32) float32 {
	var sum float32
	for index := range n.combs {
		sum += n.combs[index].process(input)
	}
	for index := range n.allpasses {
		sum = n.allpasses[index].process(sum)
	}
	return sum
}

func scaledLength(reference, channel, rate int) int {
	length := int(math.Round(float64(reference+channel*channelSpread) * float64(rate) / referenceRate))
	return max(length, 1)
}

type reverb struct {
	wet, dry float32
	networks []reverbNetwork
}

func (r *reverb) Apply(planes [][]float32) {
	for channel, samples := range planes {
		network := &r.networks[channel]
		for index, input := range samples {
			samples[index] = input*r.dry + network.process(input*combGain)*r.wet
		}
	}
}
