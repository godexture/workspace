package registry

import (
	"fmt"
)

type DecoderManifest struct {
	TransformManifest
	Factory DecoderFactory
}

func (m DecoderManifest) Validate() error {
	if err := m.TransformManifest.validate(); err != nil {
		return err
	}
	if m.Factory == nil {
		return fmt.Errorf("decoder manifest %q has no factory", m.Name)
	}
	return nil
}
