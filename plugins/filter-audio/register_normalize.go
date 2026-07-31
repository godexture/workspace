package filter

import (
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
	"github.com/godexture/godec/plugins/filter-audio/internal/normalize"
)

func init() {
	registerSimple[config.NormalizeConfig, NormalizeConfig](registry.NewConfigurationFactory(NewNormalizeConfig), "normalize", "Normalize peak level", normalize.New)
}
