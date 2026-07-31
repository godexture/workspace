package registry

import "fmt"

// ManifestFactory builds the concrete FilterManifest for one resolved set
// of parameters. It runs once per resolution (e.g. once per CLI --filter
// invocation), after Parameters is decoded but before the regular
// per-instance Configuration is decoded — the returned manifest's own
// ConfigurationFactory determines that Configuration's type, and may
// itself depend on the parameters (e.g. a mixer's input/output port count
// determining how many named input ports its InputRequirements declares).
type ManifestFactory func(parameters Configuration) (FilterManifest, error)

// ParameterizedFilterManifest is a higher-order filter registration: its
// registered identity (via BaseManifest.ConfigurationFactory) is a
// *parameters* type, not the eventual per-instance Configuration type.
// NewManifest is called with the resolved parameters to produce the
// concrete FilterManifest used for one invocation; that manifest is never
// itself added to a Registry[FilterManifest] — it is used directly and
// discarded once the invocation finishes.
//
// This lets a single registered name (e.g. "mixer") support a family of
// topologies chosen at resolution time (e.g. an arbitrary input/output
// port count), without pre-registering one manifest per shape, and
// without capping how many ports a shape may have.
//
// Parameters is deliberately not named "Shape": a future parameterized
// manifest may need structural choices that aren't port counts at all, so
// nothing here assumes the parameters are about topology specifically.
type ParameterizedFilterManifest struct {
	BaseManifest
	NewManifest ManifestFactory
}

func (m ParameterizedFilterManifest) Validate() error {
	if err := m.BaseManifest.validate(); err != nil {
		return err
	}
	if m.NewManifest == nil {
		return fmt.Errorf("parameterized filter manifest %q has no manifest factory", m.Name)
	}
	return nil
}
