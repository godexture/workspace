package filter

import (
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/gain"
)

func init() {
	registerSimple[config.GainConfig, GainConfig](registry.NewConfigurationFactory(NewGainConfig), "gain", "Adjust audio gain", gain.New)
}
