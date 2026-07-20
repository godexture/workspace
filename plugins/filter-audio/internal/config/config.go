package config

import (
	"fmt"
	"math"
	"time"

	"github.com/godexture/core/domain/media"
	mediapcm "github.com/godexture/core/domain/media/pcm"
)

const defaultMemoryLimitBytes int64 = 64 << 20

type FormatConfig struct {
	Format        media.SampleFormat
	BitsPerSample int
}

func (c FormatConfig) EffectiveBitsPerSample() int {
	return media.EffectiveBitsPerSample(c.Format, c.BitsPerSample)
}

type ResampleConfig struct {
	SampleRate int
}

type RemixConfig struct {
	Layout        media.ChannelLayout
	CenterMixDB   float64
	SurroundMixDB float64
	LFEMixDB      float64
	Normalize     bool
}

type GainConfig struct {
	Decibels float64
}

type NormalizeConfig struct {
	TargetPeakDBFS     float64
	AllowAmplification bool
	MemoryLimitBytes   int64
	TempDir            string
}

type FadeConfig struct {
	FadeIn           time.Duration
	FadeOut          time.Duration
	MemoryLimitBytes int64
	TempDir          string
}

type DCOffsetConfig struct {
	Pole float64
}

type TrimConfig struct {
	ThresholdDBFS    float64
	MemoryLimitBytes int64
	TempDir          string
}

var (
	DefaultFormatConfig   = FormatConfig{}
	DefaultResampleConfig = ResampleConfig{}
	DefaultRemixConfig    = RemixConfig{
		CenterMixDB:   -3.010299956639812,
		SurroundMixDB: -3.010299956639812,
		LFEMixDB:      math.Inf(-1),
		Normalize:     true,
	}
	DefaultGainConfig      = GainConfig{}
	DefaultNormalizeConfig = NormalizeConfig{
		TargetPeakDBFS:     -1,
		AllowAmplification: true,
		MemoryLimitBytes:   defaultMemoryLimitBytes,
	}
	DefaultFadeConfig     = FadeConfig{MemoryLimitBytes: defaultMemoryLimitBytes}
	DefaultDCOffsetConfig = DCOffsetConfig{Pole: 0.995}
	DefaultTrimConfig     = TrimConfig{ThresholdDBFS: -60, MemoryLimitBytes: defaultMemoryLimitBytes}
)

func (c FormatConfig) Validate() error {
	if err := mediapcm.ValidateFormat(c.Format); err != nil {
		return err
	}
	if c.BitsPerSample < 0 || c.BitsPerSample > c.Format.BytesPerSample()*8 {
		return fmt.Errorf("invalid bits per sample: %d", c.BitsPerSample)
	}
	return nil
}

func (c ResampleConfig) Validate() error {
	if c.SampleRate <= 0 {
		return fmt.Errorf("sample rate must be positive: %d", c.SampleRate)
	}
	return nil
}

func (c RemixConfig) Validate() error {
	if c.Layout.ChannelCount() <= 0 {
		return fmt.Errorf("target layout must have at least one channel")
	}
	if err := c.Layout.Validate(); err != nil {
		return fmt.Errorf("invalid target layout: %w", err)
	}
	if !finite(c.CenterMixDB) || !finite(c.SurroundMixDB) || (!math.IsInf(c.LFEMixDB, -1) && !finite(c.LFEMixDB)) {
		return fmt.Errorf("mix levels must be finite or negative infinity for LFE")
	}
	return nil
}

func (c GainConfig) Validate() error {
	if !finite(c.Decibels) {
		return fmt.Errorf("gain must be finite")
	}
	return nil
}

func (c NormalizeConfig) Validate() error {
	if !finite(c.TargetPeakDBFS) || c.TargetPeakDBFS > 0 {
		return fmt.Errorf("target peak must be finite and no greater than 0 dBFS")
	}
	return validateMemoryLimit(c.MemoryLimitBytes)
}

func (c FadeConfig) Validate() error {
	if c.FadeIn < 0 || c.FadeOut < 0 {
		return fmt.Errorf("fade durations must not be negative")
	}
	return validateMemoryLimit(c.MemoryLimitBytes)
}

func (c DCOffsetConfig) Validate() error {
	if !finite(c.Pole) || c.Pole <= 0 || c.Pole >= 1 {
		return fmt.Errorf("DC offset pole must be in (0, 1)")
	}
	return nil
}

func (c TrimConfig) Validate() error {
	if !finite(c.ThresholdDBFS) || c.ThresholdDBFS > 0 {
		return fmt.Errorf("trim threshold must be finite and no greater than 0 dBFS")
	}
	return validateMemoryLimit(c.MemoryLimitBytes)
}

func validateMemoryLimit(value int64) error {
	if value <= 0 {
		return fmt.Errorf("memory limit must be positive: %d", value)
	}
	return nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
