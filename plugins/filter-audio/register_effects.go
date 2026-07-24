package filter

import (
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"github.com/godexture/filter-audio/internal/compressor"
	"github.com/godexture/filter-audio/internal/config"
	"github.com/godexture/filter-audio/internal/dcoffset"
	"github.com/godexture/filter-audio/internal/equalizer"
	"github.com/godexture/filter-audio/internal/fade"
	"github.com/godexture/filter-audio/internal/gain"
	"github.com/godexture/filter-audio/internal/gate"
	"github.com/godexture/filter-audio/internal/normalize"
	"github.com/godexture/filter-audio/internal/retime"
	"github.com/godexture/filter-audio/internal/trim"
	"github.com/godexture/sdk/engine"
)

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
	register(registry.NewConfigurationFactory(NewDCOffsetConfig), "remove-dc-offset", "Remove DC offset", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
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
func registerGate() {
	register(registry.NewConfigurationFactory(NewGateConfig), "gate", "Silence samples below a threshold (hard cut or Buchla-style low-pass gate)", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.GateConfig, GateConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := gate.New(value)
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
	register(registry.NewConfigurationFactory(NewSpeedConfig), "retime", "Change playback retime (pitch shifts with retime)", func(in media.StreamInfo, cfg registry.Configuration) (media.Profile, error) {
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

func registerCompressor() {
	register(registry.NewConfigurationFactory(NewCompressorConfig), "compressor", "Reduce dynamic range above a threshold", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.CompressorConfig, CompressorConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := compressor.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}

func registerEQ() {
	register(registry.NewConfigurationFactory(NewEqualizerConfig), "equalizer", "Apply a single-band parametric, shelf, or pass biquad filter", identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[config.EqualizerConfig, EqualizerConfig](cfg)
		if err != nil {
			return nil, err
		}
		item, err := equalizer.New(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}
