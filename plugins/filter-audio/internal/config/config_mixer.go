package config

import "fmt"

// MixerParameters chooses a mixer's port topology: Inputs input ports
// ("in0".."in{Inputs-1}") and Outputs output ports ("out0".."out{Outputs-1}").
// It is resolved once per invocation, before MixerConfig is even decoded,
// since a mixer's actual port set — and therefore what InputRequirements
// its manifest declares — depends on these values. See
// registry.ParameterizedFilterManifest.
type MixerParameters struct {
	Inputs  int `name:"in" help:"Number of input ports"`
	Outputs int `name:"out" help:"Number of output ports"`
}

var DefaultMixerParameters = MixerParameters{Inputs: 1, Outputs: 1}

func (p MixerParameters) Validate() error {
	if p.Inputs < 1 {
		return fmt.Errorf("mixer must have at least one input")
	}
	if p.Outputs < 1 {
		return fmt.Errorf("mixer must have at least one output")
	}
	return nil
}

// MixerConfig has no per-instance settings yet: the CLI-registered mixer
// always mixes every input with equal weight (see register_mixer.go), so
// there is nothing left to configure once MixerParameters has fixed the
// port topology. Custom per-input weighting stays a Go-API-only concern
// (mixer.New), reached by constructing a node.Filter directly instead of
// through this registration.
type MixerConfig struct{}

var DefaultMixerConfig = MixerConfig{}

func (MixerConfig) Validate() error { return nil }
