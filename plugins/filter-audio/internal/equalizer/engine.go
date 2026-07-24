package equalizer

import (
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/sdk/audio"
	"github.com/godexture/sdk/buffer"
)

type biquad struct {
	b0, b1, b2, a1, a2 float32
}

// channelState holds the two-sample history a Direct Form I biquad needs,
// kept per channel since each channel is filtered independently.
type channelState struct {
	x1, x2, y1, y2 float32
}

type Engine struct {
	cfg     config.EqualizerConfig
	coeffs  biquad
	state   []channelState
	rateSet bool
	rate    int
	slot    buffer.Slot[media.Frame]
	scratch audio.Scratch
}

func New(cfg config.EqualizerConfig) (*Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Engine{cfg: cfg}, nil
}

func (e *Engine) ensureCoefficients(rate int) error {
	if e.rateSet {
		if e.rate != rate {
			return fmt.Errorf("equalizer input sample rate changed within stream")
		}
		return nil
	}
	e.coeffs = computeBiquad(e.cfg, rate)
	e.rate = rate
	e.rateSet = true
	return nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return fmt.Errorf("equalizer received nil frame")
	}
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("equalizer expected *media.AudioFrame, got %T", *frame)
	}
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	if err := e.ensureCoefficients(block.Rate); err != nil {
		return err
	}
	if len(e.state) != len(block.Channels) {
		e.state = make([]channelState, len(block.Channels))
	}
	c := e.coeffs
	for channel, values := range block.Channels {
		s := &e.state[channel]
		for i, x0 := range values {
			y0 := c.b0*x0 + c.b1*s.x1 + c.b2*s.x2 - c.a1*s.y1 - c.a2*s.y2
			s.x2, s.x1 = s.x1, x0
			s.y2, s.y1 = s.y1, y0
			values[i] = y0
		}
	}
	output, err := audio.EncodeInto(block, input.Format, input.BitsPerSample, &e.scratch)
	if err != nil {
		return err
	}
	return e.slot.Push(output)
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) {
	frame, err := e.slot.Receive()
	if err != nil {
		return nil, err
	}
	return &frame, nil
}
func (e *Engine) Flush() error { e.slot.Flush(); return nil }
func (e *Engine) Close() error { e.slot.Close(); return nil }

// computeBiquad derives normalized (a0 == 1) Direct Form I coefficients using
// the RBJ Audio Equalizer Cookbook formulas, parameterized directly by Q (including
// for the shelving shapes, rather than the cookbook's alternate slope form).
func computeBiquad(cfg config.EqualizerConfig, rate int) biquad {
	w0 := 2 * math.Pi * cfg.FrequencyHz / float64(rate)
	cosW0, sinW0 := math.Cos(w0), math.Sin(w0)
	alpha := sinW0 / (2 * cfg.Q)
	a := math.Pow(10, cfg.GainDB/40)

	var b0, b1, b2, a0, a1, a2 float64
	switch cfg.Type {
	case config.EqualizerTypeLowShelf:
		sqrtA := math.Sqrt(a)
		b0 = a * ((a + 1) - (a-1)*cosW0 + 2*sqrtA*alpha)
		b1 = 2 * a * ((a - 1) - (a+1)*cosW0)
		b2 = a * ((a + 1) - (a-1)*cosW0 - 2*sqrtA*alpha)
		a0 = (a + 1) + (a-1)*cosW0 + 2*sqrtA*alpha
		a1 = -2 * ((a - 1) + (a+1)*cosW0)
		a2 = (a + 1) + (a-1)*cosW0 - 2*sqrtA*alpha
	case config.EqualizerTypeHighShelf:
		sqrtA := math.Sqrt(a)
		b0 = a * ((a + 1) + (a-1)*cosW0 + 2*sqrtA*alpha)
		b1 = -2 * a * ((a - 1) + (a+1)*cosW0)
		b2 = a * ((a + 1) + (a-1)*cosW0 - 2*sqrtA*alpha)
		a0 = (a + 1) - (a-1)*cosW0 + 2*sqrtA*alpha
		a1 = 2 * ((a - 1) - (a+1)*cosW0)
		a2 = (a + 1) - (a-1)*cosW0 - 2*sqrtA*alpha
	case config.EqualizerTypeLowPass:
		b0 = (1 - cosW0) / 2
		b1 = 1 - cosW0
		b2 = (1 - cosW0) / 2
		a0 = 1 + alpha
		a1 = -2 * cosW0
		a2 = 1 - alpha
	case config.EqualizerTypeHighPass:
		b0 = (1 + cosW0) / 2
		b1 = -(1 + cosW0)
		b2 = (1 + cosW0) / 2
		a0 = 1 + alpha
		a1 = -2 * cosW0
		a2 = 1 - alpha
	default: // EqualizerTypePeaking
		b0 = 1 + alpha*a
		b1 = -2 * cosW0
		b2 = 1 - alpha*a
		a0 = 1 + alpha/a
		a1 = -2 * cosW0
		a2 = 1 - alpha/a
	}
	return biquad{
		b0: float32(b0 / a0),
		b1: float32(b1 / a0),
		b2: float32(b2 / a0),
		a1: float32(a1 / a0),
		a2: float32(a2 / a0),
	}
}
