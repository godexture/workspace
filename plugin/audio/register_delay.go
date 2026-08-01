package filter

import (
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/delay"
	"github.com/godexture/godec/sdk/engine"
)

func init() {
	register(registry.NewConfigurationFactory(NewDelayConfig), "delay", "Apply a feedback delay (echo)", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.DelayConfig, DelayConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := delay.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}
