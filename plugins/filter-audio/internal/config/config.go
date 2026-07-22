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
	Format        media.SampleFormat `name:"format" help:"Target sample format"`
	BitsPerSample int                `name:"bits-per-sample" help:"Target effective bit depth"`
}

func (c FormatConfig) EffectiveBitsPerSample() int {
	return media.EffectiveBitsPerSample(c.Format, c.BitsPerSample)
}

type ResampleConfig struct {
	SampleRate int `name:"sample-rate" help:"Target sample rate"`
}

type RemixConfig struct {
	Layout        media.ChannelLayout `name:"layout" help:"Target channel layout"`
	CenterMixDB   float64             `name:"center-mix-db" help:"Center channel mix level"`
	SurroundMixDB float64             `name:"surround-mix-db" help:"Surround channel mix level"`
	LFEMixDB      float64             `name:"lfe-mix-db" help:"LFE channel mix level"`
	Normalize     bool                `name:"normalize" help:"Normalize remix levels"`
}

type GainConfig struct {
	Decibels float64 `name:"decibels" help:"Gain in dB"`
}

type NormalizeConfig struct {
	TargetPeakDBFS     float64 `name:"target-peak-dbfs" help:"Target peak level"`
	AllowAmplification bool    `name:"allow-amplification" help:"Allow gain above unity"`
	MemoryLimitBytes   int64   `name:"memory-limit-bytes" help:"Maximum buffered memory"`
	TempDir            string  `name:"temp-dir" help:"Temporary directory"`
}

type FadeConfig struct {
	FadeIn           time.Duration `name:"fade-in" help:"Fade-in duration"`
	FadeOut          time.Duration `name:"fade-out" help:"Fade-out duration"`
	MemoryLimitBytes int64         `name:"memory-limit-bytes" help:"Maximum buffered memory"`
	TempDir          string        `name:"temp-dir" help:"Temporary directory"`
}

type DCOffsetConfig struct {
	Pole float64 `name:"pole" help:"DC offset filter pole"`
}

type TrimConfig struct {
	ThresholdDBFS    float64 `name:"threshold-dbfs" help:"Silence threshold"`
	MemoryLimitBytes int64   `name:"memory-limit-bytes" help:"Maximum buffered memory"`
	TempDir          string  `name:"temp-dir" help:"Temporary directory"`
}

// SpeedMode selects how a SpeedConfig filter achieves its speed change.
type SpeedMode string

const (
	// SpeedModeInterpolate resamples the waveform via linear interpolation
	// and keeps the output labeled at the input sample rate. Duration and
	// pitch both change, but interpolation introduces some quality loss.
	SpeedModeInterpolate SpeedMode = "interpolate"
	// SpeedModeRelabel passes samples through untouched and only retags the
	// output sample rate, so it is lossless, but downstream consumers see a
	// stream whose sample rate has changed.
	SpeedModeRelabel SpeedMode = "relabel"
)

type SpeedConfig struct {
	Factor float64   `name:"factor" help:"Playback speed multiplier (e.g. 2 for double speed, 0.5 for half); pitch shifts with speed"`
	Mode   SpeedMode `name:"mode" help:"How speed is applied: interpolate (resample, keeps input sample rate) or relabel (no resampling, retags the sample rate; lossless)"`
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
	DefaultSpeedConfig    = SpeedConfig{Factor: 1, Mode: SpeedModeInterpolate}
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

func (c SpeedConfig) Validate() error {
	if !finite(c.Factor) || c.Factor <= 0 {
		return fmt.Errorf("speed factor must be finite and positive")
	}
	switch c.Mode {
	case SpeedModeInterpolate, SpeedModeRelabel:
	default:
		return fmt.Errorf("invalid speed mode: %q", c.Mode)
	}
	return nil
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
