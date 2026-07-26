package filter

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/remix"
	"github.com/godexture/sdk/engine"
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
