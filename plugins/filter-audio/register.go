package filter

import (
	"math"
	"time"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/convert"
	"github.com/godexture/filter-audio/internal/dcoffset"
	"github.com/godexture/filter-audio/internal/fade"
	"github.com/godexture/filter-audio/internal/gain"
	"github.com/godexture/filter-audio/internal/normalize"
	"github.com/godexture/filter-audio/internal/remix"
	"github.com/godexture/filter-audio/internal/resample"
	"github.com/godexture/filter-audio/internal/speed"
	"github.com/godexture/filter-audio/internal/trim"
	"github.com/godexture/sdk/engine"
)

func init() {
	registerConvert()
	registerResample()
	registerRemix()
	registerGain()
	registerNormalize()
	registerFade()
	registerDCOffset()
	registerTrim()
	registerSpeed()
}

func registerConvert() {
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

func registerResample() {
	register(registry.NewConfigurationFactory(NewResampleConfig), "resample", "Resample audio with linear interpolation", func(in media.StreamInfo, cfg registry.Configuration) (media.Profile, error) {
		value, err := engine.ResolveConfig[config.ResampleConfig, ResampleConfig](cfg)
		if err != nil {
			return media.Profile{}, err
		}
		profile := copyProfile(in)
		profile.Audio.SampleRate = value.SampleRate
		return profile, nil
	}, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.ResampleConfig, ResampleConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := resample.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, bridgeRate, nil)
}

func registerRemix() {
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

func registerGain() {
	register(registry.NewConfigurationFactory(NewGainConfig), "gain", "Adjust audio gain", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.GainConfig, GainConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := gain.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}
func registerNormalize() {
	register(registry.NewConfigurationFactory(NewNormalizeConfig), "normalize", "Normalize peak level", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.NormalizeConfig, NormalizeConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := normalize.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}
func registerFade() {
	register(registry.NewConfigurationFactory(NewFadeConfig), "fade", "Apply fade in and fade out", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.FadeConfig, FadeConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := fade.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}
func registerDCOffset() {
	register(registry.NewConfigurationFactory(NewDCOffsetConfig), "dc-offset", "Remove DC offset", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.DCOffsetConfig, DCOffsetConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := dcoffset.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}
func registerTrim() {
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

func registerSpeed() {
	register(registry.NewConfigurationFactory(NewSpeedConfig), "speed", "Change playback speed (pitch shifts with speed)", func(in media.StreamInfo, cfg registry.Configuration) (media.Profile, error) {
		value, err := engine.ResolveConfig[config.SpeedConfig, SpeedConfig](cfg)
		if err != nil {
			return media.Profile{}, err
		}
		profile := copyProfile(in)
		if value.Mode == SpeedModeRelabel {
			profile.Audio.SampleRate = speedRelabelRate(in.Audio.SampleRate, value.Factor)
		}
		return profile, nil
	}, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.SpeedConfig, SpeedConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := speed.New(value)
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

func speedRelabelRate(rate int, factor float64) int {
	target := int(math.Round(float64(rate) * factor))
	if target < 1 {
		target = 1
	}
	return target
}

func register(newConfig registry.ConfigurationFactory, name, description string, transform func(media.StreamInfo, registry.Configuration) (media.Profile, error), factory func(registry.Configuration) (node.Filter, error), bridge registry.BridgeFunc, transformStream func(media.StreamInfo, media.CodecID, registry.Configuration) (media.StreamInfo, error)) {
	if err := godec.Register(registry.FilterManifest{TransformManifest: registry.TransformManifest{BaseManifest: registry.BaseManifest{Name: name, Description: description, ConfigurationFactory: newConfig}, InputRequirements: registry.StaticRequirements(&manifest.AudioConstraint{}), TransformFunc: func(in media.StreamInfo, _ media.CodecID, cfg registry.Configuration) (media.Profile, error) {
		return transform(in, cfg)
	}, TransformStreamFunc: transformStream}, Bridge: bridge, Factory: func(_ media.StreamInfo, options registry.TransformFactoryOptions) (node.Filter, error) {
		return factory(options.Config)
	}}); err != nil {
		panic(err)
	}
}

func copyProfile(in media.StreamInfo) media.Profile {
	return media.Profile{Type: in.Type, MediaAttributes: in.MediaAttributes}
}
func identityTransform(in media.StreamInfo, _ registry.Configuration) (media.Profile, error) {
	return copyProfile(in), nil
}
