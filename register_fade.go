package filter

import (
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/fade"
)

func init() {
	registerSimple[config.FadeConfig, FadeConfig](registry.NewConfigurationFactory(NewFadeConfig), "fade", "Apply fade in and fade out", fade.New)
}
