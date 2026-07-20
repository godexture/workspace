package filter

import (
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/filter-audio/internal/config"
)

type FormatConfig config.FormatConfig
type ResampleConfig config.ResampleConfig
type RemixConfig config.RemixConfig
type GainConfig config.GainConfig
type NormalizeConfig config.NormalizeConfig
type FadeConfig config.FadeConfig
type DCOffsetConfig config.DCOffsetConfig
type TrimConfig config.TrimConfig

type FormatConfigOption func(*FormatConfig)
type ResampleConfigOption func(*ResampleConfig)
type RemixConfigOption func(*RemixConfig)
type GainConfigOption func(*GainConfig)
type NormalizeConfigOption func(*NormalizeConfig)
type FadeConfigOption func(*FadeConfig)
type DCOffsetConfigOption func(*DCOffsetConfig)
type TrimConfigOption func(*TrimConfig)

func NewFormatConfig(options ...FormatConfigOption) FormatConfig {
	result := FormatConfig(config.DefaultFormatConfig)
	for _, option := range options {
		option(&result)
	}
	return result
}

func NewResampleConfig(options ...ResampleConfigOption) ResampleConfig {
	result := ResampleConfig(config.DefaultResampleConfig)
	for _, option := range options {
		option(&result)
	}
	return result
}

func NewRemixConfig(options ...RemixConfigOption) RemixConfig {
	result := RemixConfig(config.DefaultRemixConfig)
	for _, option := range options {
		option(&result)
	}
	return result
}

func NewGainConfig(options ...GainConfigOption) GainConfig {
	result := GainConfig(config.DefaultGainConfig)
	for _, option := range options {
		option(&result)
	}
	return result
}

func NewNormalizeConfig(options ...NormalizeConfigOption) NormalizeConfig {
	result := NormalizeConfig(config.DefaultNormalizeConfig)
	for _, option := range options {
		option(&result)
	}
	return result
}

func NewFadeConfig(options ...FadeConfigOption) FadeConfig {
	result := FadeConfig(config.DefaultFadeConfig)
	for _, option := range options {
		option(&result)
	}
	return result
}

func NewDCOffsetConfig(options ...DCOffsetConfigOption) DCOffsetConfig {
	result := DCOffsetConfig(config.DefaultDCOffsetConfig)
	for _, option := range options {
		option(&result)
	}
	return result
}

func NewTrimConfig(options ...TrimConfigOption) TrimConfig {
	result := TrimConfig(config.DefaultTrimConfig)
	for _, option := range options {
		option(&result)
	}
	return result
}

func (c FormatConfig) ResolveDefault() config.FormatConfig     { return config.DefaultFormatConfig }
func (c FormatConfig) Resolve() config.FormatConfig            { return config.FormatConfig(c) }
func (c ResampleConfig) ResolveDefault() config.ResampleConfig { return config.DefaultResampleConfig }
func (c ResampleConfig) Resolve() config.ResampleConfig        { return config.ResampleConfig(c) }
func (c RemixConfig) ResolveDefault() config.RemixConfig       { return config.DefaultRemixConfig }
func (c RemixConfig) Resolve() config.RemixConfig              { return config.RemixConfig(c) }
func (c GainConfig) ResolveDefault() config.GainConfig         { return config.DefaultGainConfig }
func (c GainConfig) Resolve() config.GainConfig                { return config.GainConfig(c) }
func (c NormalizeConfig) ResolveDefault() config.NormalizeConfig {
	return config.DefaultNormalizeConfig
}
func (c NormalizeConfig) Resolve() config.NormalizeConfig      { return config.NormalizeConfig(c) }
func (c FadeConfig) ResolveDefault() config.FadeConfig         { return config.DefaultFadeConfig }
func (c FadeConfig) Resolve() config.FadeConfig                { return config.FadeConfig(c) }
func (c DCOffsetConfig) ResolveDefault() config.DCOffsetConfig { return config.DefaultDCOffsetConfig }
func (c DCOffsetConfig) Resolve() config.DCOffsetConfig        { return config.DCOffsetConfig(c) }
func (c TrimConfig) ResolveDefault() config.TrimConfig         { return config.DefaultTrimConfig }
func (c TrimConfig) Resolve() config.TrimConfig                { return config.TrimConfig(c) }

func WithFormat(value media.SampleFormat) FormatConfigOption {
	return func(c *FormatConfig) { c.Format = value }
}

func WithBitsPerSample(value int) FormatConfigOption {
	return func(c *FormatConfig) { c.BitsPerSample = value }
}

func WithSampleRate(value int) ResampleConfigOption {
	return func(c *ResampleConfig) { c.SampleRate = value }
}

func WithLayout(value media.ChannelLayout) RemixConfigOption {
	return func(c *RemixConfig) { c.Layout = value }
}

func WithCenterMixDB(value float64) RemixConfigOption {
	return func(c *RemixConfig) { c.CenterMixDB = value }
}

func WithSurroundMixDB(value float64) RemixConfigOption {
	return func(c *RemixConfig) { c.SurroundMixDB = value }
}

func WithLFEMixDB(value float64) RemixConfigOption {
	return func(c *RemixConfig) { c.LFEMixDB = value }
}

func WithRemixNormalize(value bool) RemixConfigOption {
	return func(c *RemixConfig) { c.Normalize = value }
}

func WithDecibels(value float64) GainConfigOption {
	return func(c *GainConfig) { c.Decibels = value }
}

func WithTargetPeakDBFS(value float64) NormalizeConfigOption {
	return func(c *NormalizeConfig) { c.TargetPeakDBFS = value }
}

func WithAllowAmplification(value bool) NormalizeConfigOption {
	return func(c *NormalizeConfig) { c.AllowAmplification = value }
}

func WithNormalizeMemoryLimitBytes(value int64) NormalizeConfigOption {
	return func(c *NormalizeConfig) { c.MemoryLimitBytes = value }
}

func WithNormalizeTempDir(value string) NormalizeConfigOption {
	return func(c *NormalizeConfig) { c.TempDir = value }
}

func WithFadeIn(value time.Duration) FadeConfigOption {
	return func(c *FadeConfig) { c.FadeIn = value }
}

func WithFadeOut(value time.Duration) FadeConfigOption {
	return func(c *FadeConfig) { c.FadeOut = value }
}

func WithFadeMemoryLimitBytes(value int64) FadeConfigOption {
	return func(c *FadeConfig) { c.MemoryLimitBytes = value }
}

func WithFadeTempDir(value string) FadeConfigOption {
	return func(c *FadeConfig) { c.TempDir = value }
}

func WithDCPole(value float64) DCOffsetConfigOption {
	return func(c *DCOffsetConfig) { c.Pole = value }
}

func WithThresholdDBFS(value float64) TrimConfigOption {
	return func(c *TrimConfig) { c.ThresholdDBFS = value }
}

func WithTrimMemoryLimitBytes(value int64) TrimConfigOption {
	return func(c *TrimConfig) { c.MemoryLimitBytes = value }
}

func WithTrimTempDir(value string) TrimConfigOption {
	return func(c *TrimConfig) { c.TempDir = value }
}
