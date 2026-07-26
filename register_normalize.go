package filter

import (
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/normalize"
)

func init() {
	registerSimple[config.NormalizeConfig, NormalizeConfig](registry.NewConfigurationFactory(NewNormalizeConfig), "normalize", "Normalize peak level", normalize.New)
}
