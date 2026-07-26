package filter

import (
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/dcoffset"
)

func init() {
	registerSimple[config.DCOffsetConfig, DCOffsetConfig](registry.NewConfigurationFactory(NewDCOffsetConfig), "remove-dc-offset", "Remove DC offset", dcoffset.New)
}
