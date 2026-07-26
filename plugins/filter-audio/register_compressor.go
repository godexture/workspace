package filter

import (
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/compressor"
	"github.com/godexture/filter-audio/internal/config"
)

func init() {
	registerSimple[config.CompressorConfig, CompressorConfig](registry.NewConfigurationFactory(NewCompressorConfig), "compressor", "Reduce dynamic range above a threshold", compressor.New)
}
