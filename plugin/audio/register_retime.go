package filter

import (
	"time"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/plugin/audio/internal/config"
	"github.com/godexture/godec/plugin/audio/internal/retime"
	"github.com/godexture/godec/sdk/engine"
)

func init() {
	register(registry.NewConfigurationFactory(NewSpeedConfig), "retime", "Change playback speed with optional pitch-preserving retime", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.SpeedConfig, SpeedConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := retime.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, func(in media.StreamInfo, _ media.CodecID, cfg registry.Configuration) (media.StreamInfo, error) {
		value, err := engine.ResolveConfig[config.SpeedConfig, SpeedConfig](cfg)
		if err != nil {
			return media.StreamInfo{}, err
		}
		if in.Duration > 0 {
			in.Duration = time.Duration(float64(in.Duration) / value.Factor)
		}
		if value.Mode == SpeedModeRelabel {
			in.Audio.SampleRate = speedRelabelRate(in.Audio.SampleRate, value.Factor)
		}
		return in, nil
	})
}
