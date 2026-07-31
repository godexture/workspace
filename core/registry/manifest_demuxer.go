package registry

import (
	"fmt"

	"github.com/godexture/godec/core/domain/manifest"
)

type DemuxerManifest struct {
	BaseManifest
	Probe   manifest.Prober
	Factory DemuxerFactory
}

func (m DemuxerManifest) Validate() error {
	if err := m.BaseManifest.validate(); err != nil {
		return err
	}
	if m.Probe == nil {
		return fmt.Errorf("demuxer manifest %q has no probe", m.Name)
	}
	if m.Factory == nil {
		return fmt.Errorf("demuxer manifest %q has no factory", m.Name)
	}
	return nil
}
