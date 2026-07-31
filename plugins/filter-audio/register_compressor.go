package filter

import (
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugins/filter-audio/internal/compressor"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
)

func init() {
	registerSimple[config.CompressorConfig, CompressorConfig](registry.NewConfigurationFactory(NewCompressorConfig), "compressor", "Reduce dynamic range above a threshold", compressor.New)
}
