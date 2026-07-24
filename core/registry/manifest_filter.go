package registry

import (
	"fmt"
	"slices"

	"github.com/godexture/core/domain/media"
)

type FilterManifest struct {
	TransformManifest
	// OutputPorts declares the named outputs produced by Factory. Keeping the
	// topology with the manifest lets callers describe and validate a filter
	// before a stream has been negotiated.
	OutputPorts []string
	Bridge      map[string]BridgeFunc
	Factory     FilterFactory
}

// ValidateOutputs verifies that a factory result matches the manifest's
// declared topology. Empty declarations are tolerated here for unregistered
// test manifests; registered manifests are rejected by Validate.
func (m FilterManifest) ValidateOutputs(outputs media.StreamSet) error {
	if len(m.OutputPorts) == 0 {
		return nil
	}
	if len(outputs) != len(m.OutputPorts) {
		return fmt.Errorf("filter manifest %q returned %d output ports, want %d", m.Name, len(outputs), len(m.OutputPorts))
	}
	for _, port := range m.OutputPorts {
		if _, ok := outputs[port]; !ok {
			return fmt.Errorf("filter manifest %q did not return output port %q", m.Name, port)
		}
	}
	return nil
}

func (m FilterManifest) Validate() error {
	if err := m.TransformManifest.validate(); err != nil {
		return err
	}
	if len(m.OutputPorts) == 0 || slices.Contains(m.OutputPorts, "") || hasDuplicates(m.OutputPorts) {
		return fmt.Errorf("filter manifest %q has invalid output ports", m.Name)
	}
	for port, bridge := range m.Bridge {
		if _, ok := m.InputRequirements[port]; !ok || bridge == nil {
			return fmt.Errorf("filter manifest %q has invalid bridge for port %q", m.Name, port)
		}
	}
	if m.Factory == nil {
		return fmt.Errorf("filter manifest %q has no factory", m.Name)
	}
	return nil
}
