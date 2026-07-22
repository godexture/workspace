package filter

import (
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/reverb"
	"github.com/godexture/sdk/engine"
)

func init() {
	registerReverb()
}

func registerReverb() {
	register(registry.NewConfigurationFactory(NewReverbConfig), "reverb", "Apply a Freeverb-style algorithmic reverb", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.ReverbConfig, ReverbConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := reverb.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}
