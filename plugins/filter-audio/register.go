package filter

import (
	"math"

	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/convert"
	"github.com/godexture/filter-audio/internal/remix"
	"github.com/godexture/filter-audio/internal/resample"
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
	registerGate()
	registerTrim()
	registerRetime()
	registerCompressor()
	registerEQ()
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

func speedRelabelRate(rate int, factor float64) int {
	target := int(math.Round(float64(rate) * factor))
	if target < 1 {
		target = 1
	}
	return target
}

func register(newConfig registry.ConfigurationFactory, name, description string, transform func(media.StreamInfo, registry.Configuration) (media.Profile, error), factory func(registry.Configuration) (node.Filter, error), bridge registry.BridgeFunc, transformStream func(media.StreamInfo, media.CodecID, registry.Configuration) (media.StreamInfo, error)) {
	if err := godec.Register(registry.FilterManifest{TransformManifest: registry.TransformManifest{BaseManifest: registry.BaseManifest{Name: name, Description: description, ConfigurationFactory: newConfig}, InputRequirements: registry.SingleInputRequirements(registry.StaticRequirements(&manifest.AudioConstraint{}))}, OutputPorts: []string{"out"}, Bridge: registry.SingleInputBridge(bridge), Factory: registry.SingleFactory(func(in media.StreamInfo, options registry.TransformFactoryOptions) (node.Filter, media.StreamInfo, error) {
		item, err := factory(options.Config)
		if err != nil {
			return nil, media.StreamInfo{}, err
		}
		if transformStream != nil {
			output, err := transformStream(in, in.Codec, options.Config)
			if err != nil {
				_ = item.Close()
				return nil, media.StreamInfo{}, err
			}
			return item, output, nil
		}
		profile, err := transform(in, options.Config)
		if err != nil {
			_ = item.Close()
			return nil, media.StreamInfo{}, err
		}
		in.Type = profile.Type
		in.MediaAttributes = profile.MediaAttributes
		return item, in, nil
	})}); err != nil {
		panic(err)
	}
}

func copyProfile(in media.StreamInfo) media.Profile {
	return media.Profile{Type: in.Type, MediaAttributes: in.MediaAttributes}
}
func identityTransform(in media.StreamInfo, _ registry.Configuration) (media.Profile, error) {
	return copyProfile(in), nil
}
