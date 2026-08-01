package filter

import (
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/reverb"
	"github.com/godexture/godec/sdk/engine"
)

func init() {
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
