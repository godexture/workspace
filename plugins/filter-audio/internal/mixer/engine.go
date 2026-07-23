// Package mixer implements an N-input, M-output linear mixing matrix. A
// plain N-to-1 mixer and a 1-to-M tee are both special cases of the same
// matrix (an all-ones row, or an all-ones column, respectively), so one
// engine serves both.
package mixer

import (
	"fmt"
	"math"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/sdk/audio"
	"github.com/godexture/sdk/dsp"
	"github.com/godexture/sdk/engine"
)

// portState is one input port's accumulation state: pending holds samples
// received but not yet mixed, one slice per channel; ended is set once the
// port's EndInput has been called.
type portState struct {
	pending audio.Channels
	ended   bool
}

type outputItem struct {
	port  string
	frame media.Frame
}

// Engine mixes its input ports into its output ports using a fixed weight
// matrix: out[m] = sum_n weights[m][n] * in[n]. Every port shares the same
// sample rate, format, bit depth, and channel count — reconciling channel
// layouts across ports is the existing remix filter's job, not this one's.
//
// Ports advance independently and are aligned by each port's own first
// received frame, not by absolute PTS: sample i of every port is the i-th
// sample that port has produced, counting from when it started. A port
// that ends before the others contributes silence for the remainder, so
// mixing continues until every port has ended and every buffered sample
// has been consumed.
type Engine struct {
	weights   [][]float64
	inputIDs  []string
	outputIDs []string
	ports     map[string]*portState

	rateSet  bool
	rate     int
	format   media.SampleFormat
	bits     int
	layout   media.ChannelLayout
	channels int

	basePTS      media.Pts
	totalEmitted int64
	pending      []outputItem
	flushed      bool
}

// NewEngine builds a mixer for exactly n inputs and m outputs.
// weights[o][i] is the gain applied to input i when producing output o.
//
// weights may be nil only for the two shapes with an unambiguous default:
// m == 1 (every input summed with weight 1) or n == 1 (every output an
// identical copy of the input, i.e. a tee). Any other combination of n and
// m requires an explicit matrix. If normalize is true, each output row
// with an L1 norm above 1 is scaled down to avoid clipping (see
// pkg/dsp.ClampL1); rows already within bound are left untouched.
func NewEngine(n, m int, weights [][]float64, normalize bool) (*Engine, error) {
	if n < 1 {
		return nil, fmt.Errorf("mixer must have at least one input")
	}
	if m < 1 {
		return nil, fmt.Errorf("mixer must have at least one output")
	}
	if weights == nil {
		weights = defaultWeights(n, m)
		if weights == nil {
			return nil, fmt.Errorf("mixer weights are required when n=%d and m=%d (no unambiguous default)", n, m)
		}
	}
	if len(weights) != m {
		return nil, fmt.Errorf("mixer weights has %d output rows, want %d", len(weights), m)
	}
	for o, row := range weights {
		if len(row) != n {
			return nil, fmt.Errorf("mixer weights row %d has %d entries, want %d", o, len(row), n)
		}
		for i, w := range row {
			if math.IsNaN(w) || math.IsInf(w, 0) {
				return nil, fmt.Errorf("mixer weight [%d][%d] must be finite", o, i)
			}
		}
	}
	if normalize {
		weights = dsp.ClampL1(weights)
	}

	inputIDs := portNames("in", n)
	outputIDs := portNames("out", m)
	ports := make(map[string]*portState, n)
	for _, id := range inputIDs {
		ports[id] = &portState{}
	}

	return &Engine{
		weights:   weights,
		inputIDs:  inputIDs,
		outputIDs: outputIDs,
		ports:     ports,
	}, nil
}

// New builds a ready-to-wire node.Filter: same as NewEngine, but already
// wrapped with n input ports ("in0".."in{n-1}") and m output ports
// ("out0".."out{m-1}").
func New(n, m int, weights [][]float64, normalize bool) (node.Filter, error) {
	eng, err := NewEngine(n, m, weights, normalize)
	if err != nil {
		return nil, err
	}
	inputs := make([]engine.FilterInput, len(eng.inputIDs))
	for i, id := range eng.inputIDs {
		inputs[i] = engine.FilterInput{ID: id, Phase: node.InputPhaseRun}
	}
	return engine.WrapFilter(eng, engine.WithInputs(inputs...), engine.WithOutputs(eng.outputIDs...)), nil
}

// defaultWeights returns the unambiguous weight matrix for the two
// degenerate shapes (m == 1 or n == 1), or nil if neither applies.
func defaultWeights(n, m int) [][]float64 {
	switch {
	case m == 1:
		row := make([]float64, n)
		for i := range row {
			row[i] = 1
		}
		return [][]float64{row}
	case n == 1:
		rows := make([][]float64, m)
		for o := range rows {
			rows[o] = []float64{1}
		}
		return rows
	default:
		return nil
	}
}

func portNames(prefix string, count int) []string {
	names := make([]string, count)
	for i := range names {
		names[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return names
}

func (e *Engine) SendFrame(frame *media.Frame) error { return e.SendInput(e.inputIDs[0], frame) }

func (e *Engine) SendInput(port string, frame *media.Frame) error {
	state, ok := e.ports[port]
	if !ok {
		return fmt.Errorf("mixer has no input port %q", port)
	}
	if state.ended {
		return fmt.Errorf("mixer received a frame on port %q after it ended", port)
	}
	block, err := audio.Decode(frame)
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
		for m := range outputs {
			outputs[m] = make(audio.Channels, e.channels)
			for c := range outputs[m] {
				outputs[m][c] = make([]float32, length)
			}
		}
		for n, id := range e.inputIDs {
			state := e.ports[id]
			hasPending := pendingLen(state) > 0
			for m := range e.outputIDs {
				w := float32(e.weights[m][n])
				if w == 0 || !hasPending {
					continue
				}
				for c := 0; c < e.channels; c++ {
					dst := outputs[m][c]
					src := state.pending[c]
					for i := 0; i < length; i++ {
						dst[i] += w * src[i]
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
		for m, id := range e.outputIDs {
			if err := e.pushOutput(id, outputs[m], pts); err != nil {
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
	frame, err := audio.Encode(block, e.format, e.bits)
	if err != nil {
		return err
	}
	e.pending = append(e.pending, outputItem{port: port, frame: frame})
	return nil
}

func (e *Engine) ReceiveOutput() (string, *media.Frame, error) {
	if len(e.pending) == 0 {
		if e.flushed {
			return "", nil, engine.ErrEOF
		}
		return "", nil, engine.ErrEAGAIN
	}
	item := e.pending[0]
	e.pending = e.pending[1:]
	return item.port, &item.frame, nil
}

func (e *Engine) ReceiveFrame() (*media.Frame, error) {
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
