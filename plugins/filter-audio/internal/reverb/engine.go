package reverb

import (
	"fmt"
	"math"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/buffer"
)

// Freeverb-style algorithmic reverb: per channel, numCombs parallel comb
// filters (each a delay line with a one-pole low-pass in its feedback path,
// giving the decaying tail its high-frequency damping) feed numAllpasses
// series allpass filters, which diffuse the comb outputs into a smooth,
// non-metallic tail. Tuning constants are Schroeder/Jezar's well-known
// values, scaled from their reference 44.1kHz sample count to the input rate.
const (
	numCombs      = 8
	numAllpasses  = 4
	referenceRate = 44100

	// stereoSpread staggers each channel's delay lengths so multi-channel
	// input decorrelates into a wider tail instead of every channel
	// reverberating identically (Freeverb's classic L/R offset trick,
	// generalized here to however many channels the input has).
	stereoSpreadSamples = 23

	allpassFeedback = 0.5
	// fixedGain keeps the comb network's amplified feedback from clipping.
	fixedGain = 0.015

	scaleRoom  = 0.28
	offsetRoom = 0.7
	scaleDamp  = 0.4
)

var combTuning = [numCombs]int{1116, 1188, 1277, 1356, 1422, 1491, 1557, 1617}
var allpassTuning = [numAllpasses]int{556, 441, 341, 225}

type comb struct {
	buffer      []float32
	index       int
	filterStore float32
	feedback    float32
	damp1       float32
	damp2       float32
}

func (c *comb) process(input float32) float32 {
	output := c.buffer[c.index]
	c.filterStore = output*c.damp2 + c.filterStore*c.damp1
	c.buffer[c.index] = input + c.filterStore*c.feedback
	c.index++
	if c.index == len(c.buffer) {
		c.index = 0
	}
	return output
}

type allpass struct {
	buffer   []float32
	index    int
	feedback float32
}

func (a *allpass) process(input float32) float32 {
	bufout := a.buffer[a.index]
	output := bufout - input
	a.buffer[a.index] = input + bufout*a.feedback
	a.index++
	if a.index == len(a.buffer) {
		a.index = 0
	}
	return output
}

type channelNetwork struct {
	combs     [numCombs]comb
	allpasses [numAllpasses]allpass
}

func newChannelNetwork(channel, rate int, feedback, damp1, damp2 float32) *channelNetwork {
	n := &channelNetwork{}
	for i := range n.combs {
		n.combs[i] = comb{
			buffer:   make([]float32, scaleDelay(combTuning[i], channel, rate)),
			feedback: feedback,
			damp1:    damp1,
			damp2:    damp2,
		}
	}
	for i := range n.allpasses {
		n.allpasses[i] = allpass{
			buffer:   make([]float32, scaleDelay(allpassTuning[i], channel, rate)),
			feedback: allpassFeedback,
		}
	}
	return n
}

func scaleDelay(baseAt44100, channel, rate int) int {
	length := int(math.Round(float64(baseAt44100+channel*stereoSpreadSamples) * float64(rate) / referenceRate))
	if length < 1 {
		length = 1
	}
	return length
}

func (n *channelNetwork) process(input float32) float32 {
	var sum float32
	for i := range n.combs {
		sum += n.combs[i].process(input)
	}
	for i := range n.allpasses {
		sum = n.allpasses[i].process(sum)
	}
	return sum
}

type Engine struct {
	cfg      config.ReverbConfig
	feedback float32
	damp1    float32
	damp2    float32
	rateSet  bool
	rate     int
	networks []*channelNetwork
	slot     buffer.Slot[media.Frame]
	scratch  audio.Scratch
}

func New(cfg config.ReverbConfig) (*Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	damp1 := float32(cfg.Damping * scaleDamp)
	return &Engine{
		cfg:      cfg,
		feedback: float32(offsetRoom + cfg.RoomSize*scaleRoom),
		damp1:    damp1,
		damp2:    1 - damp1,
	}, nil
}

func (e *Engine) ensureRate(rate int) error {
	if e.rateSet {
		if e.rate != rate {
			return fmt.Errorf("reverb input sample rate changed within stream")
		}
		return nil
	}
	e.rate = rate
	e.rateSet = true
	return nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return fmt.Errorf("reverb received nil frame")
	}
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("reverb expected *media.AudioFrame, got %T", *frame)
	}
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	if err := e.ensureRate(block.Rate); err != nil {
		return err
	}
	if len(e.networks) != len(block.Channels) {
		e.networks = make([]*channelNetwork, len(block.Channels))
		for channel := range e.networks {
			e.networks[channel] = newChannelNetwork(channel, e.rate, e.feedback, e.damp1, e.damp2)
		}
	}
	wet := float32(e.cfg.WetLevel)
	dry := float32(e.cfg.DryLevel)
	for channel, values := range block.Channels {
		network := e.networks[channel]
		for i, x := range values {
			wetSample := network.process(x * fixedGain)
			values[i] = x*dry + wetSample*wet
		}
	}
	output, err := audio.EncodeInto(block, input.Format, input.BitsPerSample, &e.scratch)
	if err != nil {
		return err
	}
	return e.slot.Push(output)
}

func (e *Engine) ReceiveFrame() (media.Frame, error) {
	return e.slot.Receive()
}
func (e *Engine) Flush() error { e.slot.Flush(); return nil }
func (e *Engine) Close() error { e.slot.Close(); return nil }
