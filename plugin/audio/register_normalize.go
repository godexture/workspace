package filter

import (
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/normalize"
)

func init() {
	registerSimple[config.NormalizeConfig, NormalizeConfig](registry.NewConfigurationFactory(NewNormalizeConfig), "normalize", "Normalize peak level", normalize.New)
}
