package convolver

import (
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/sdk/audio"
)

func (e *Engine) ensureChannels(block audio.Block) error {
	if e.rateSet {
		return e.validateInput(block)
	}
	if e.irRate != 0 && e.irRate != block.Rate {
		return fmt.Errorf("convolution impulse response rate %d does not match input rate %d; resample the impulse response before use", e.irRate, block.Rate)
	}
	n := len(block.Channels)
	if len(e.partitions) != 1 && len(e.partitions) != n {
		return fmt.Errorf("convolution impulse response has %d channels, want 1 or %d", len(e.partitions), n)
	}

	e.rateSet = true
	e.rate = block.Rate
	e.format = block.Format
	e.bits = block.Bits
	e.layout = block.Layout
	e.basePTS = block.PTS

	e.channels = make([]channelState, n)
	for ch := range e.channels {
		depth := len(e.partitionsFor(ch))
		delayLine := make([][]complex64, depth)
		for i := range delayLine {
			delayLine[i] = make([]complex64, e.bins)
		}
		e.channels[ch] = channelState{
			history:    make([]float32, e.hop),
			window:     make([]float32, 2*e.hop),
			timeDomain: make([]float32, 2*e.hop),
			delayLine:  delayLine,
			accum:      make([]complex64, e.bins),
		}
	}
	return nil
}

func (e *Engine) validateInput(block audio.Block) error {
	if block.Rate != e.rate || block.Format != e.format || block.Bits != e.bits || block.Layout != e.layout {
		return fmt.Errorf("convolver input format changed within stream")
	}
	if len(block.Channels) != len(e.channels) {
		return fmt.Errorf("convolver input channel count changed within stream")
	}
	if block.PTS != e.basePTS+media.Pts(e.totalInput) {
		return fmt.Errorf("convolver input PTS discontinuity: got %d, want %d", block.PTS, e.basePTS+media.Pts(e.totalInput))
	}
	return nil
}

// processHops consumes complete hops from every channel's pending buffer
// (all channels always have equal pending length, since they are filled
// together from the same decoded blocks) until less than a hop remains.
func (e *Engine) processHops() error {
	for len(e.channels[0].pending) >= e.hop {
		output := make(audio.Channels, len(e.channels))
		for ch := range e.channels {
			out, err := e.processHop(ch)
			if err != nil {
				return err
			}
			output[ch] = out
		}
		if err := e.pushBlock(output); err != nil {
			return err
		}
	}
	return nil
}

// processHop consumes one hop's worth of pending samples on channel ch,
// advances its overlap-save window and frequency-domain delay line, and
// returns the hop's output samples (after wet/dry mixing).
func (e *Engine) processHop(ch int) ([]float32, error) {
	state := &e.channels[ch]
	newBlock := state.pending[:e.hop]

	copy(state.window[:e.hop], state.history)
	copy(state.window[e.hop:], newBlock)

	spectrum := state.delayLine[state.head]
	if err := e.plan.Forward(spectrum, state.window); err != nil {
		return nil, err
	}

	for k := range state.accum {
		state.accum[k] = 0
	}
	partitions := e.partitionsFor(ch)
	depth := len(state.delayLine)
	for p, part := range partitions {
		idx := state.head - p
		if idx < 0 {
			idx += depth
		}
		src := state.delayLine[idx]
		for k := range state.accum {
			state.accum[k] += src[k] * part.spectrum[k]
		}
	}

	if err := e.plan.Inverse(state.timeDomain, state.accum); err != nil {
		return nil, err
	}

	out := make([]float32, e.hop)
	copy(out, state.timeDomain[e.hop:])

	if mix := float32(e.cfg.WetDryMix); mix != 1 {
		dry := 1 - mix
		for i := range out {
			out[i] = newBlock[i]*dry + out[i]*mix
		}
	}

	copy(state.history, newBlock)
	state.head = (state.head + 1) % depth
	state.pending = state.pending[e.hop:]

	return out, nil
}

func (e *Engine) pushBlock(channels audio.Channels) error {
	block := audio.Block{
		Channels: channels,
		Layout:   e.layout,
		Rate:     e.rate,
		Format:   e.format,
		Bits:     e.bits,
		PTS:      e.basePTS + media.Pts(e.hopsEmitted)*media.Pts(e.hop),
	}
	e.hopsEmitted++
	frame, err := audio.EncodeInto(block, e.format, e.bits, &e.scratch)
	if err != nil {
		return err
	}
	return e.queue.Push(frame)
}
