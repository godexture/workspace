package config

import (
	"fmt"
	"math"

	"github.com/godexture/godec/core/domain/media"
)

type RemixConfig struct {
	Layout        media.ChannelLayout `name:"layout" help:"Target channel layout"`
	CenterMixDB   float64             `name:"center-mix-db" check:"finite" help:"Center channel mix level"`
	SurroundMixDB float64             `name:"surround-mix-db" check:"finite" help:"Surround channel mix level"`
	LFEMixDB      float64             `name:"lfe-mix-db" help:"LFE channel mix level"`
	Normalize     bool                `name:"normalize" help:"Normalize remix levels"`
}

var DefaultRemixConfig = RemixConfig{
	CenterMixDB:   -3.010299956639812,
	SurroundMixDB: -3.010299956639812,
	LFEMixDB:      math.Inf(-1),
	Normalize:     true,
}

func (c RemixConfig) Validate() error {
	if c.Layout.ChannelCount() <= 0 {
		return fmt.Errorf("target layout must have at least one channel")
	}
	if err := c.Layout.Validate(); err != nil {
		return fmt.Errorf("invalid target layout: %w", err)
	}
	if !math.IsInf(c.LFEMixDB, -1) && !finite(c.LFEMixDB) {
		return fmt.Errorf("mix levels must be finite or negative infinity for LFE")
	}
	return nil
}
