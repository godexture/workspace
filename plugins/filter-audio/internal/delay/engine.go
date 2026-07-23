package delay

import (
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/sdk/audio"
	"github.com/godexture/sdk/engine"
)

// Engine is a feedback delay line: each channel gets its own circular buffer
// of delaySamples length, sharing a single write index since every channel
// has the same delay time. Feedback controls how much of the delayed signal
// is written back in, turning a single repeat into a decaying series of
// echoes; wet/dry control how the delayed and original signals are mixed.
type Engine struct {
	cfg          config.DelayConfig
	rateSet      bool
	rate         int
	delaySamples int
	buffers      [][]float32
	writeIndex   int
	slot         engine.Slot[media.Frame]
}

func New(cfg config.DelayConfig) (*Engine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Engine{cfg: cfg}, nil
}

func (e *Engine) ensureRate(rate int) error {
	if e.rateSet {
		if e.rate != rate {
			return fmt.Errorf("delay input sample rate changed within stream")
		}
		return nil
	}
	e.rate = rate
	e.delaySamples = int(math.Round(e.cfg.DelayMs / 1000 * float64(rate)))
	if e.delaySamples < 1 {
		e.delaySamples = 1
	}
	e.rateSet = true
	return nil
}

func (e *Engine) SendFrame(frame *media.Frame) error {
	if frame == nil || *frame == nil {
		return fmt.Errorf("delay received nil frame")
	}
	input, ok := (*frame).(*media.AudioFrame)
	if !ok {
		return fmt.Errorf("delay expected *media.AudioFrame, got %T", *frame)
	}
	block, err := audio.Decode(frame)
	if err != nil {
		return err
	}
	if err := e.ensureRate(block.Rate); err != nil {
		return err
	}
	if len(e.buffers) != len(block.Channels) {
		e.buffers = make([][]float32, len(block.Channels))
		for channel := range e.buffers {
			e.buffers[channel] = make([]float32, e.delaySamples)
		}
		e.writeIndex = 0
	}
	feedback := float32(e.cfg.Feedback)
	wet := float32(e.cfg.WetLevel)
	dry := float32(e.cfg.DryLevel)
	for i := 0; i < block.Samples(); i++ {
		index := e.writeIndex
		for channel, values := range block.Channels {
			buffer := e.buffers[channel]
			delayed := buffer[index]
			buffer[index] = values[i] + delayed*feedback
			values[i] = values[i]*dry + delayed*wet
		}
		e.writeIndex++
		if e.writeIndex == e.delaySamples {
			e.writeIndex = 0
		}
	}
	output, err := audio.Encode(block, input.Format, input.BitsPerSample)
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
