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
// matrix: out[o] = sum_i weights[o][i] * in[i]. Every port shares the same
// sample rate, format, bit depth, and channel count — reconciling channel
// layouts across ports is the existing remix filter's job, not this one's.
//
// Ports advance independently and are aligned by each port's own first
// received frame, not by absolute PTS: sample k of every port is the k-th
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

	// scratch is safe to share across every input port: node.Filter's
	// adapter (pkg/engine.FilterAdapter.runGeneral) pulls each port on its
	// own goroutine but always invokes SendInput/ReceiveOutput from a single
	// consumer goroutine, so this Engine's own methods are never called
	// concurrently with each other.
	scratch audio.Scratch
}

// NewEngine builds a mixer for exactly the given number of inputs and
// outputs. weights[o][i] is the gain applied to input i when producing
// output o.
//
// weights may be nil only for the two shapes with an unambiguous default:
// outputs == 1 (every input summed with weight 1) or inputs == 1 (every
// output an identical copy of the input, i.e. a tee). Any other
// combination requires an explicit matrix. If normalize is true, each
// output row with an L1 norm above 1 is scaled down to avoid clipping (see
// pkg/dsp.ClampL1); rows already within bound are left untouched.
func NewEngine(inputs, outputs int, weights [][]float64, normalize bool) (*Engine, error) {
	if inputs < 1 {
		return nil, fmt.Errorf("mixer must have at least one input")
	}
	if outputs < 1 {
		return nil, fmt.Errorf("mixer must have at least one output")
	}
	if weights == nil {
		weights = defaultWeights(inputs, outputs)
		if weights == nil {
			return nil, fmt.Errorf("mixer weights are required when inputs=%d and outputs=%d (no unambiguous default)", inputs, outputs)
		}
	}
	if len(weights) != outputs {
		return nil, fmt.Errorf("mixer weights has %d output rows, want %d", len(weights), outputs)
	}
	for o, row := range weights {
		if len(row) != inputs {
			return nil, fmt.Errorf("mixer weights row %d has %d entries, want %d", o, len(row), inputs)
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

	inputIDs := portNames("in", inputs)
	outputIDs := portNames("out", outputs)
	ports := make(map[string]*portState, inputs)
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
// wrapped with the given number of input ports ("in0".."in{inputs-1}") and
// output ports ("out0".."out{outputs-1}").
func New(inputs, outputs int, weights [][]float64, normalize bool) (node.Filter, error) {
	eng, err := NewEngine(inputs, outputs, weights, normalize)
	if err != nil {
		return nil, err
	}
	filterInputs := make([]engine.FilterInput, len(eng.inputIDs))
	for i, id := range eng.inputIDs {
		filterInputs[i] = engine.FilterInput{ID: id, Phase: node.InputPhaseRun}
	}
	return engine.WrapFilter(eng, engine.WithInputs(filterInputs...), engine.WithOutputs(eng.outputIDs...)), nil
}

// defaultWeights returns the unambiguous weight matrix for the two
// degenerate shapes (outputs == 1 or inputs == 1), or nil if neither
// applies.
func defaultWeights(inputs, outputs int) [][]float64 {
	switch {
	case outputs == 1:
		row := make([]float64, inputs)
		for i := range row {
			row[i] = 1
		}
		return [][]float64{row}
	case inputs == 1:
		rows := make([][]float64, outputs)
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
