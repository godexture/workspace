package config

// MixerParameters chooses a mixer's port topology: Inputs input ports
// ("in0".."in{Inputs-1}") and Outputs output ports ("out0".."out{Outputs-1}").
// It is resolved once per invocation, before MixerConfig is even decoded,
// since a mixer's actual port set  Eand therefore what InputRequirements
// its manifest declares  Edepends on these values. See
// registry.ParameterizedFilterManifest.
type MixerParameters struct {
	Inputs  int `name:"in" check:"positive" help:"Number of input ports"`
	Outputs int `name:"out" check:"positive" help:"Number of output ports"`
}

var DefaultMixerParameters = MixerParameters{Inputs: 1, Outputs: 1}

func (p MixerParameters) Validate() error {
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
