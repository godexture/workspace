package config

import "fmt"

type CompressorConfig struct {
	ThresholdDBFS float64 `name:"threshold-dbfs" help:"Level above which compression begins"`
	Ratio         float64 `name:"ratio" help:"Compression ratio applied above the threshold (e.g. 4 for 4:1)"`
	AttackMs      float64 `name:"attack-ms" help:"Time to react to level increases, in milliseconds"`
	ReleaseMs     float64 `name:"release-ms" help:"Time to recover after level drops, in milliseconds"`
	KneeDB        float64 `name:"knee-db" help:"Soft-knee width in dB around the threshold (0 for a hard knee)"`
	MakeupGainDB  float64 `name:"makeup-gain-db" help:"Gain applied after compression to restore level"`
}

var DefaultCompressorConfig = CompressorConfig{ThresholdDBFS: -18, Ratio: 4, AttackMs: 10, ReleaseMs: 100, KneeDB: 6}

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
