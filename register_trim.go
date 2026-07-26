package filter

import (
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/trim"
	"github.com/godexture/sdk/engine"
)

func init() {
	register(registry.NewConfigurationFactory(NewTrimConfig), "trim", "Trim silence from the start, end, or both", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.TrimConfig, TrimConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := trim.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, func(in media.StreamInfo, _ media.CodecID, _ registry.Configuration) (media.StreamInfo, error) {
		in.Duration = 0
		return in, nil
	})
}
