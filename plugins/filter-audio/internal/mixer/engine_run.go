package mixer

import (
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/sdk/audio"
	"github.com/godexture/godec/sdk/engine"
)

func (e *Engine) SendFrame(frame *media.Frame) error { return e.SendInput(e.inputIDs[0], frame) }

func (e *Engine) SendInput(port string, frame *media.Frame) error {
	state, ok := e.ports[port]
	if !ok {
		return fmt.Errorf("mixer has no input port %q", port)
	}
	if state.ended {
		return fmt.Errorf("mixer received a frame on port %q after it ended", port)
	}
	block, err := audio.DecodeInto(frame, &e.scratch)
	if err != nil {
		return err
	}
	if err := e.ensureFormat(block); err != nil {
		return err
	}
	if len(state.pending) == 0 {
		state.pending = make(audio.Channels, e.channels)
	}
	for c := range state.pending {
		state.pending[c] = append(state.pending[c], block.Channels[c]...)
	}
	return e.mixAvailable()
}

func (e *Engine) EndInput(port string) error {
	state, ok := e.ports[port]
	if !ok {
		return fmt.Errorf("mixer has no input port %q", port)
	}
	if state.ended {
		return fmt.Errorf("mixer input port %q already ended", port)
	}
	state.ended = true
	return e.mixAvailable()
}

func (e *Engine) ensureFormat(block audio.Block) error {
	channels := len(block.Channels)
	if !e.rateSet {
		e.rateSet = true
		e.rate = block.Rate
		e.format = block.Format
		e.bits = block.Bits
		e.layout = block.Layout
		e.channels = channels
		e.basePTS = block.PTS
		return nil
	}
	if block.Rate != e.rate || block.Format != e.format || block.Bits != e.bits {
		return fmt.Errorf("mixer input format changed within stream")
	}
	if channels != e.channels {
		return fmt.Errorf("mixer input has %d channels, want %d (all mixer ports must share a channel count; remix beforehand to reconcile layouts)", channels, e.channels)
	}
	return nil
}

// pendingLen reports how many buffered samples remain on a port, correctly
// reporting 0 both before the port's first frame (pending is nil) and
// after every buffered sample has been consumed (pending's channel slices
// are non-nil but drained to length 0) — unlike len(state.pending) alone,
// which is the channel count and stays nonzero once allocated.
func pendingLen(state *portState) int {
	if len(state.pending) == 0 {
		return 0
	}
	return len(state.pending[0])
}

// mixAvailable produces as much mixed output as currently possible: it
// repeatedly finds the largest sample count safe to consume from every
// port that is still live (an ended, drained port contributes silence and
// never gates this), mixes that many samples for every output, and repeats
// until no live port has more buffered data.
func (e *Engine) mixAvailable() error {
	if !e.rateSet {
		return nil
	}
	for {
		length := -1
		for _, id := range e.inputIDs {
			state := e.ports[id]
			available := pendingLen(state)
			if state.ended && available == 0 {
				continue
			}
			if length == -1 || available < length {
				length = available
			}
		}
		if length <= 0 {
			return nil
		}

		outputs := make([]audio.Channels, len(e.outputIDs))
		for o := range outputs {
			outputs[o] = make(audio.Channels, e.channels)
			for c := range outputs[o] {
				outputs[o][c] = make([]float32, length)
			}
		}
		for i, id := range e.inputIDs {
			state := e.ports[id]
			hasPending := pendingLen(state) > 0
			for o := range e.outputIDs {
				w := float32(e.weights[o][i])
				if w == 0 || !hasPending {
					continue
				}
				for c := 0; c < e.channels; c++ {
					dst := outputs[o][c]
					src := state.pending[c]
					for k := 0; k < length; k++ {
						dst[k] += w * src[k]
					}
				}
			}
			if hasPending {
				for c := range state.pending {
					state.pending[c] = state.pending[c][length:]
				}
			}
		}

		pts := e.basePTS + media.Pts(e.totalEmitted)
		for o, id := range e.outputIDs {
			if err := e.pushOutput(id, outputs[o], pts); err != nil {
				return err
			}
		}
		e.totalEmitted += int64(length)
	}
}

func (e *Engine) pushOutput(port string, channels audio.Channels, pts media.Pts) error {
	block := audio.Block{
		Channels: channels,
		Layout:   e.layout,
		Rate:     e.rate,
		Format:   e.format,
		Bits:     e.bits,
		PTS:      pts,
	}
	frame, err := audio.EncodeInto(block, e.format, e.bits, &e.scratch)
	if err != nil {
		return err
	}
	e.pending = append(e.pending, outputItem{port: port, frame: frame})
	return nil
}

func (e *Engine) ReceiveOutput() (string, media.Frame, error) {
	if len(e.pending) == 0 {
		if e.flushed {
			return "", nil, engine.ErrEOF
		}
		return "", nil, engine.ErrEAGAIN
	}
	item := e.pending[0]
	e.pending = e.pending[1:]
	return item.port, item.frame, nil
}

func (e *Engine) ReceiveFrame() (media.Frame, error) {
	_, frame, err := e.ReceiveOutput()
	return frame, err
}

func (e *Engine) Flush() error {
	e.flushed = true
	return nil
}

func (e *Engine) Close() error {
	for _, item := range e.pending {
		item.frame.Release()
	}
	e.pending = nil
	return nil
}
