package config

import (
	"fmt"
	"math"
)

// MaxMixerPorts bounds both the input and output port counts a mixer
// manifest declares statically at registration time. Weights determines the
// actual topology per instance; a Factory only ever attaches as many ports
// as Weights calls for, up to this bound.
const MaxMixerPorts = 8

// MixerConfig configures an N-input, M-output linear mixing matrix (N and M
// up to MaxMixerPorts): each output is a weighted sum of the inputs,
// out[m] = sum_n Weights[m][n] * in[n]. A plain N-to-1 mixer and a 1-to-M
// tee are both special cases of the same matrix (an all-ones row, or an
// all-ones column, respectively). Input ports are named "in0".."inN-1";
// output ports are named "out0".."outM-1".
type MixerConfig struct {
	Weights   [][]float64
	Normalize bool `name:"normalize" help:"Scale down any output row whose weights could sum to more than unity gain, to avoid clipping"`
}

var DefaultMixerConfig = MixerConfig{Normalize: true}

func (c MixerConfig) Validate() error {
	if len(c.Weights) == 0 {
		return fmt.Errorf("mixer must have at least one output")
	}
	if len(c.Weights) > MaxMixerPorts {
		return fmt.Errorf("mixer supports at most %d outputs, got %d", MaxMixerPorts, len(c.Weights))
	}
	inputs := len(c.Weights[0])
	if inputs == 0 {
		return fmt.Errorf("mixer must have at least one input")
	}
	if inputs > MaxMixerPorts {
		return fmt.Errorf("mixer supports at most %d inputs, got %d", MaxMixerPorts, inputs)
	}
	for m, row := range c.Weights {
		if len(row) != inputs {
			return fmt.Errorf("mixer weight row %d has %d entries, want %d", m, len(row), inputs)
		}
		for n, w := range row {
			if math.IsNaN(w) || math.IsInf(w, 0) {
				return fmt.Errorf("mixer weight [%d][%d] must be finite", m, n)
			}
		}
	}
	return nil
}
