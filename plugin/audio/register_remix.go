package filter

import (
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/remix"
	"github.com/godexture/godec/sdk/engine"
)

func init() {
	register(registry.NewConfigurationFactory(NewRemixConfig), "remix", "Remix audio channel layout", func(in media.StreamInfo, cfg registry.Configuration) (media.Profile, error) {
		value, err := engine.ResolveConfig[config.RemixConfig, RemixConfig](cfg)
		if err != nil {
			return media.Profile{}, err
		}
		profile := copyProfile(in)
		profile.Audio.ChannelLayout = value.Layout
		return profile, nil
	}, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.RemixConfig, RemixConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := remix.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, bridgeLayout, nil)
}
