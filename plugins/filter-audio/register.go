package filter

import (
	godec "github.com/godexture/core"
	"github.com/godexture/core/domain/manifest"
	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	filterconfig "github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/convert"
	"github.com/godexture/filter-audio/internal/dcoffset"
	"github.com/godexture/filter-audio/internal/fade"
	"github.com/godexture/filter-audio/internal/gain"
	"github.com/godexture/filter-audio/internal/normalize"
	"github.com/godexture/filter-audio/internal/remix"
	"github.com/godexture/filter-audio/internal/resample"
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
}

func registerConvert() {
	register(NewFormatConfig(), "audio-convert", "Convert audio sample format", func(in media.StreamInfo, cfg registry.Configuration) (media.Profile, error) {
		value, err := engine.ResolveConfig[filterconfig.FormatConfig, FormatConfig](cfg)
		if err != nil {
			return media.Profile{}, err
		}
		profile := copyProfile(in)
		profile.Audio.Format, profile.Audio.BitsPerSample = value.Format, value.BitsPerSample
		return profile, nil
	}, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[filterconfig.FormatConfig, FormatConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := convert.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, bridgeFormat)
}

func registerResample() {
	register(NewResampleConfig(), "audio-resample", "Resample audio with linear interpolation", func(in media.StreamInfo, cfg registry.Configuration) (media.Profile, error) {
		value, err := engine.ResolveConfig[filterconfig.ResampleConfig, ResampleConfig](cfg)
		if err != nil {
			return media.Profile{}, err
		}
		profile := copyProfile(in)
		profile.Audio.SampleRate = value.SampleRate
		return profile, nil
	}, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[filterconfig.ResampleConfig, ResampleConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := resample.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, bridgeRate)
}

func registerRemix() {
	register(NewRemixConfig(), "audio-remix", "Remix audio channel layout", func(in media.StreamInfo, cfg registry.Configuration) (media.Profile, error) {
		value, err := engine.ResolveConfig[filterconfig.RemixConfig, RemixConfig](cfg)
		if err != nil {
			return media.Profile{}, err
		}
		profile := copyProfile(in)
		profile.Audio.ChannelLayout = value.Layout
		return profile, nil
	}, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[filterconfig.RemixConfig, RemixConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := remix.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, bridgeLayout)
}

func registerGain() {
	register(NewGainConfig(), "audio-gain", "Adjust audio gain", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[filterconfig.GainConfig, GainConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := gain.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil)
}
func registerNormalize() {
	register(NewNormalizeConfig(), "audio-normalize", "Normalize peak level", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[filterconfig.NormalizeConfig, NormalizeConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := normalize.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil)
}
func registerFade() {
	register(NewFadeConfig(), "audio-fade", "Apply fade in and fade out", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[filterconfig.FadeConfig, FadeConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := fade.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil)
}
func registerDCOffset() {
	register(NewDCOffsetConfig(), "audio-dc-offset", "Remove DC offset", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[filterconfig.DCOffsetConfig, DCOffsetConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := dcoffset.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil)
}
func registerTrim() {
	register(NewTrimConfig(), "audio-trim", "Trim leading and trailing silence", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[filterconfig.TrimConfig, TrimConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := trim.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil)
}

func register(config registry.Configuration, name, description string, transform func(media.StreamInfo, registry.Configuration) (media.Profile, error), factory func(registry.Configuration) (node.Filter, error), bridge registry.BridgeFunc) {
	if err := godec.Register(config, registry.FilterManifest{TransformManifest: registry.TransformManifest{BaseManifest: registry.BaseManifest{Name: name, Description: description}, Capabilities: []manifest.Capability{&manifest.AudioConstraint{}}, TransformFunc: func(in media.StreamInfo, _ media.CodecID, cfg registry.Configuration) (media.Profile, error) {
		return transform(in, cfg)
	}}, Bridge: bridge, Factory: func(_ media.StreamInfo, options registry.TransformFactoryOptions) (node.Filter, error) {
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

func bridgeFormat(current media.StreamInfo, required []manifest.Capability) ([]registry.ConversionCandidate, error) {
	var result []registry.ConversionCandidate
	for _, capability := range required {
		constraint, ok := capability.(*manifest.AudioConstraint)
		if !ok {
			continue
		}
		for _, target := range constraint.SampleFormats {
			bits := target.BitsPerSample.Preferred(current.Audio.BitsPerSample)
			if bits == 0 {
				bits = target.Format.BytesPerSample() * 8
			}
			if target.Format != current.Audio.Format || bits != current.Audio.BitsPerSample {
				result = append(result, registry.ConversionCandidate{Config: NewFormatConfig(WithFormat(target.Format), WithBitsPerSample(bits)), Cost: registry.ConversionCost{QualityLoss: formatLoss(current.Audio.Format, target.Format), Work: 1}})
			}
		}
	}
	return result, nil
}

func bridgeRate(current media.StreamInfo, required []manifest.Capability) ([]registry.ConversionCandidate, error) {
	var result []registry.ConversionCandidate
	for _, capability := range required {
		constraint, ok := capability.(*manifest.AudioConstraint)
		if !ok {
			continue
		}
		for _, rate := range constraint.SampleRates.Candidates(current.Audio.SampleRate) {
			if rate != current.Audio.SampleRate {
				result = append(result, registry.ConversionCandidate{Config: NewResampleConfig(WithSampleRate(rate)), Cost: registry.ConversionCost{QualityLoss: 1, Work: 2}})
			}
		}
	}
	return result, nil
}

func bridgeLayout(current media.StreamInfo, required []manifest.Capability) ([]registry.ConversionCandidate, error) {
	var result []registry.ConversionCandidate
	for _, capability := range required {
		constraint, ok := capability.(*manifest.AudioConstraint)
		if !ok {
			continue
		}
		for _, layout := range constraint.Layouts {
			if layout != current.Audio.ChannelLayout {
				result = append(result, registry.ConversionCandidate{Config: NewRemixConfig(WithLayout(layout)), Cost: registry.ConversionCost{QualityLoss: 1, Work: 2}})
			}
		}
		for _, channels := range constraint.Channels.Candidates(current.Audio.ChannelCount()) {
			if channels != current.Audio.ChannelCount() {
				result = append(result, registry.ConversionCandidate{Config: NewRemixConfig(WithLayout(layoutForChannels(channels))), Cost: registry.ConversionCost{QualityLoss: 1, Work: 2}})
			}
		}
	}
	return result, nil
}

func formatLoss(from, to media.SampleFormat) uint32 {
	if isFloat(from) && !isFloat(to) {
		return 1
	}
	return 0
}
func isFloat(format media.SampleFormat) bool {
	return format.Packed() == media.SampleFormatF32 || format.Packed() == media.SampleFormatF64
}
func layoutForChannels(channels int) media.ChannelLayout {
	switch channels {
	case 1:
		return media.LayoutMono1
	case 2:
		return media.LayoutStereo2_0
	case 6:
		return media.LayoutSide5_1
	case 8:
		return media.LayoutSurround7_1
	default:
		return media.NewUnspecified(channels)
	}
}
