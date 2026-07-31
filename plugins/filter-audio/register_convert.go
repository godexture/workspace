package filter

import (
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugins/filter-audio/internal/config"
	"github.com/godexture/godec/plugins/filter-audio/internal/convert"
	"github.com/godexture/godec/sdk/engine"
)

func init() {
	register(registry.NewConfigurationFactory(NewFormatConfig), "convert", "Convert audio sample format", func(in media.StreamInfo, cfg registry.Configuration) (media.Profile, error) {
		value, err := engine.ResolveConfig[config.FormatConfig, FormatConfig](cfg)
		if err != nil {
			return media.Profile{}, err
		}
		profile := copyProfile(in)
		profile.Audio.Format, profile.Audio.BitsPerSample = value.Format, value.BitsPerSample
		return profile, nil
	}, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.FormatConfig, FormatConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := convert.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, bridgeFormat, nil)
}
