package filter

import (
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/equalizer"
)

func init() {
	registerSimple[config.EqualizerConfig, EqualizerConfig](registry.NewConfigurationFactory(NewEqualizerConfig), "equalizer", "Apply a single-band or multiband parametric/shelf/pass equalizer", equalizer.New)
}
