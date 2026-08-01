package filter

import (
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/gate"
)

func init() {
	registerSimple[config.GateConfig, GateConfig](registry.NewConfigurationFactory(NewGateConfig), "gate", "Silence samples below a threshold (hard cut or Buchla-style low-pass gate)", gate.New)
}
