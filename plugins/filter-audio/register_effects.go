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

// registerSimple registers a filter whose factory does nothing beyond
// resolving its configuration, constructing the engine, and wrapping it —
// the shape shared by every effect below that neither overrides the stream
// transform nor needs a bridge.
func registerSimple[T any, C engine.Wrapper[T], E engine.FilterEngine](newConfig registry.ConfigurationFactory, name, description string, newEngine func(T) (E, error)) {
	register(newConfig, name, description, identityTransform, func(cfg registry.Configuration) (node.Filter, error) {
		value, err := engine.ResolveConfig[T, C](cfg)
		if err != nil {
			return nil, err
		}
		item, err := newEngine(value)
		if err != nil {
			return nil, err
		}
		return engine.WrapFilter(item), nil
	}, nil, nil)
}

func registerGain() {
	registerSimple[config.GainConfig, GainConfig](registry.NewConfigurationFactory(NewGainConfig), "gain", "Adjust audio gain", gain.New)
}
func registerNormalize() {
	registerSimple[config.NormalizeConfig, NormalizeConfig](registry.NewConfigurationFactory(NewNormalizeConfig), "normalize", "Normalize peak level", normalize.New)
}
func registerFade() {
	registerSimple[config.FadeConfig, FadeConfig](registry.NewConfigurationFactory(NewFadeConfig), "fade", "Apply fade in and fade out", fade.New)
}
func registerDCOffset() {
	registerSimple[config.DCOffsetConfig, DCOffsetConfig](registry.NewConfigurationFactory(NewDCOffsetConfig), "remove-dc-offset", "Remove DC offset", dcoffset.New)
}
func registerGate() {
	registerSimple[config.GateConfig, GateConfig](registry.NewConfigurationFactory(NewGateConfig), "gate", "Silence samples below a threshold (hard cut or Buchla-style low-pass gate)", gate.New)
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

func registerRetime() {
	register(registry.NewConfigurationFactory(NewSpeedConfig), "retime", "Change playback speed with optional pitch-preserving retime", func(in media.StreamInfo, cfg registry.Configuration) (media.Profile, error) {
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
	registerSimple[config.CompressorConfig, CompressorConfig](registry.NewConfigurationFactory(NewCompressorConfig), "compressor", "Reduce dynamic range above a threshold", compressor.New)
}

func registerEQ() {
	registerSimple[config.EqualizerConfig, EqualizerConfig](registry.NewConfigurationFactory(NewEqualizerConfig), "equalizer", "Apply a single-band parametric, shelf, or pass biquad filter", equalizer.New)
}
