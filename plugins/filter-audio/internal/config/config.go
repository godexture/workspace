package config

//go:generate enum-generator

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

type TrimMode string

const (
	TrimModeBoth  TrimMode = "both"
	TrimModeStart TrimMode = "start"
	TrimModeEnd   TrimMode = "end"
)

type TrimConfig struct {
	ThresholdDBFS      float64  `name:"threshold-dbfs" help:"Silence threshold"`
	TrimMode           TrimMode `name:"mode" help:"Which end to trim: both, start, or end"`
	ApproximateSilence bool     `name:"approximate-silence" help:"Buffer trailing silence by shape only (dropping sample data) so memory stays bounded regardless of how long it runs; loses bit-exact reproduction of that silence if it turns out not to be the true end"`
	MemoryLimitBytes   int64    `name:"memory-limit-bytes" help:"Maximum buffered memory"`
	TempDir            string   `name:"temp-dir" help:"Temporary directory"`
}

type GateMode string

const (
	// GateModeHard instantly silences every sample below the threshold.
	GateModeHard GateMode = "hard"
	// GateModeLowpass is a Buchla-style low-pass gate: as the level falls
	// below the threshold, a one-pole low-pass filter and the output level
	// close together (envelope-controlled), so the sound darkens and fades
	// out smoothly instead of being cut off abruptly.
	GateModeLowpass GateMode = "lowpass"
)

type GateConfig struct {
	ThresholdDBFS    float64  `name:"threshold-dbfs" help:"Level below which the gate begins to close"`
	GateMode         GateMode `name:"mode" help:"How the gate responds: hard (instantly silences samples below the threshold) or lowpass (Buchla-style low-pass gate that darkens and fades out smoothly)"`
	RangeDB          float64  `name:"range-db" help:"dB below the threshold over which the gate fully closes (lowpass mode only)"`
	AttackMs         float64  `name:"attack-ms" help:"Time for the gate to open, in milliseconds (lowpass mode only)"`
	ReleaseMs        float64  `name:"release-ms" help:"Time for the gate to close, in milliseconds (lowpass mode only)"`
	OpenFrequencyHz  float64  `name:"open-frequency-hz" help:"Low-pass cutoff when the gate is fully open (lowpass mode only)"`
	CloseFrequencyHz float64  `name:"close-frequency-hz" help:"Low-pass cutoff when the gate is fully closed (lowpass mode only)"`
}

type SpeedMode string

const (
	SpeedModeInterpolate SpeedMode = "interpolate"
	SpeedModeRelabel     SpeedMode = "relabel"
)

type SpeedConfig struct {
	Factor float64   `name:"factor" help:"Playback speed multiplier (e.g. 2 for double speed, 0.5 for half); pitch shifts with speed"`
	Mode   SpeedMode `name:"mode" help:"How speed is applied: interpolate (resample, keeps input sample rate) or relabel (no resampling, retags the sample rate; lossless)"`
}

type CompressorConfig struct {
	ThresholdDBFS float64 `name:"threshold-dbfs" help:"Level above which compression begins"`
	Ratio         float64 `name:"ratio" help:"Compression ratio applied above the threshold (e.g. 4 for 4:1)"`
	AttackMs      float64 `name:"attack-ms" help:"Time to react to level increases, in milliseconds"`
	ReleaseMs     float64 `name:"release-ms" help:"Time to recover after level drops, in milliseconds"`
	KneeDB        float64 `name:"knee-db" help:"Soft-knee width in dB around the threshold (0 for a hard knee)"`
	MakeupGainDB  float64 `name:"makeup-gain-db" help:"Gain applied after compression to restore level"`
}

type EQType string

const (
	EQTypePeaking   EQType = "peaking"
	EQTypeLowShelf  EQType = "lowshelf"
	EQTypeHighShelf EQType = "highshelf"
	EQTypeLowPass   EQType = "lowpass"
	EQTypeHighPass  EQType = "highpass"
)

type EQConfig struct {
	Type        EQType  `name:"type" help:"Filter shape: peaking, lowshelf, highshelf, lowpass, or highpass"`
	FrequencyHz float64 `name:"frequency-hz" help:"Center frequency (peaking/shelf) or corner frequency (lowpass/highpass)"`
	GainDB      float64 `name:"gain-db" help:"Gain applied at the center frequency; ignored by lowpass/highpass"`
	Q           float64 `name:"q" help:"Filter Q; higher values narrow the band or sharpen the corner"`
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
	DefaultFadeConfig       = FadeConfig{MemoryLimitBytes: defaultMemoryLimitBytes}
	DefaultDCOffsetConfig   = DCOffsetConfig{Pole: 0.995}
	DefaultGateConfig       = GateConfig{ThresholdDBFS: -60, GateMode: GateModeHard, RangeDB: 40, AttackMs: 5, ReleaseMs: 50, OpenFrequencyHz: 20000, CloseFrequencyHz: 200}
	DefaultTrimConfig       = TrimConfig{ThresholdDBFS: -60, TrimMode: TrimModeBoth, MemoryLimitBytes: defaultMemoryLimitBytes}
	DefaultSpeedConfig      = SpeedConfig{Factor: 1, Mode: SpeedModeInterpolate}
	DefaultCompressorConfig = CompressorConfig{ThresholdDBFS: -18, Ratio: 4, AttackMs: 10, ReleaseMs: 100, KneeDB: 6}
	DefaultEQConfig         = EQConfig{Type: EQTypePeaking, FrequencyHz: 1000, Q: 0.7071067811865476}
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

func (c GateConfig) Validate() error {
	if !finite(c.ThresholdDBFS) || c.ThresholdDBFS > 0 {
		return fmt.Errorf("gate threshold must be finite and no greater than 0 dBFS")
	}
	if !c.GateMode.Valid() {
		return fmt.Errorf("invalid gate mode: %q", c.GateMode)
	}
	if c.GateMode != GateModeLowpass {
		return nil
	}
	if !finite(c.RangeDB) || c.RangeDB < 0 {
		return fmt.Errorf("gate range must be finite and non-negative")
	}
	if c.AttackMs < 0 || c.ReleaseMs < 0 {
		return fmt.Errorf("gate attack and release must not be negative")
	}
	if !finite(c.OpenFrequencyHz) || c.OpenFrequencyHz <= 0 || !finite(c.CloseFrequencyHz) || c.CloseFrequencyHz <= 0 {
		return fmt.Errorf("gate frequencies must be finite and positive")
	}
	if c.CloseFrequencyHz > c.OpenFrequencyHz {
		return fmt.Errorf("gate close frequency must not exceed open frequency")
	}
	return nil
}

func (c TrimConfig) Validate() error {
	if !finite(c.ThresholdDBFS) || c.ThresholdDBFS > 0 {
		return fmt.Errorf("trim threshold must be finite and no greater than 0 dBFS")
	}
	if !c.TrimMode.Valid() {
		return fmt.Errorf("invalid trim mode: %q", c.TrimMode)
	}
	return validateMemoryLimit(c.MemoryLimitBytes)
}

func (c SpeedConfig) Validate() error {
	if !finite(c.Factor) || c.Factor <= 0 {
		return fmt.Errorf("speed factor must be finite and positive")
	}
	if !c.Mode.Valid() {
		return fmt.Errorf("invalid speed mode: %q", c.Mode)
	}
	return nil
}

func (c CompressorConfig) Validate() error {
	if !finite(c.ThresholdDBFS) || c.ThresholdDBFS > 0 {
		return fmt.Errorf("compressor threshold must be finite and no greater than 0 dBFS")
	}
	if !finite(c.Ratio) || c.Ratio < 1 {
		return fmt.Errorf("compressor ratio must be finite and at least 1")
	}
	if c.AttackMs < 0 || c.ReleaseMs < 0 {
		return fmt.Errorf("compressor attack and release must not be negative")
	}
	if c.KneeDB < 0 {
		return fmt.Errorf("compressor knee must not be negative")
	}
	if !finite(c.MakeupGainDB) {
		return fmt.Errorf("compressor makeup gain must be finite")
	}
	return nil
}

func (c EQConfig) Validate() error {
	if !c.Type.Valid() {
		return fmt.Errorf("invalid eq type: %q", c.Type)
	}
	if !finite(c.FrequencyHz) || c.FrequencyHz <= 0 {
		return fmt.Errorf("eq frequency must be finite and positive")
	}
	if !finite(c.Q) || c.Q <= 0 {
		return fmt.Errorf("eq Q must be finite and positive")
	}
	if !finite(c.GainDB) {
		return fmt.Errorf("eq gain must be finite")
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
