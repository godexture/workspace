package filter

import (
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
	"github.com/godexture/godec/plugins/filter-audio/internal/dcoffset"
)

func init() {
	registerSimple[config.DCOffsetConfig, DCOffsetConfig](registry.NewConfigurationFactory(NewDCOffsetConfig), "remove-dc-offset", "Remove DC offset", dcoffset.New)
}
