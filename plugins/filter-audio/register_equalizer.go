package filter

import (
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/equalizer"
)

func init() {
	registerSimple[config.EqualizerConfig, EqualizerConfig](registry.NewConfigurationFactory(NewEqualizerConfig), "equalizer", "Apply a single-band or multiband parametric/shelf/pass equalizer", equalizer.New)
}
