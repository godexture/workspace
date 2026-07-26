package filter

import (
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/gate"
)

func init() {
	registerSimple[config.GateConfig, GateConfig](registry.NewConfigurationFactory(NewGateConfig), "gate", "Silence samples below a threshold (hard cut or Buchla-style low-pass gate)", gate.New)
}
