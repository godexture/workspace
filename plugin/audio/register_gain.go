package filter

import (
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/gain"
)

func init() {
	registerSimple[config.GainConfig, GainConfig](registry.NewConfigurationFactory(NewGainConfig), "gain", "Adjust audio gain", gain.New)
}
