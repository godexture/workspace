package registry

import (
	"fmt"
)

type FilterManifest struct {
	TransformManifest
	Bridge  map[string]BridgeFunc
	Factory FilterFactory
}

func (m FilterManifest) Validate() error {
	if err := m.TransformManifest.validate(); err != nil {
		return err
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
