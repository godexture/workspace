package config

import (
	"fmt"
)

type TrimMode string

const (
	TrimModeBoth  TrimMode = "both"
	TrimModeStart TrimMode = "start"
	TrimModeEnd   TrimMode = "end"
)

type TrimConfig struct {
	ThresholdDBFS      float64  `name:"threshold-dbfs" check:"finite" help:"Silence threshold"`
	TrimMode           TrimMode `name:"mode" help:"Which end to trim: both, start, or end"`
	ApproximateSilence bool     `name:"approximate-silence" help:"Buffer trailing silence by shape only (dropping sample data) so memory stays bounded regardless of how long it runs; loses bit-exact reproduction of that silence if it turns out not to be the true end"`
	MemoryLimitBytes   int64    `name:"memory-limit-bytes" help:"Maximum buffered memory"`
	TempDir            string   `name:"temp-dir" help:"Temporary directory"`
}

var DefaultTrimConfig = TrimConfig{ThresholdDBFS: -60, TrimMode: TrimModeBoth, MemoryLimitBytes: defaultMemoryLimitBytes}

func (c TrimConfig) Validate() error {
	if c.ThresholdDBFS > 0 {
		return fmt.Errorf("trim threshold must be finite and no greater than 0 dBFS")
	}
	if !c.TrimMode.Valid() {
		return fmt.Errorf("invalid trim mode: %q", c.TrimMode)
	}
	return validateMemoryLimit(c.MemoryLimitBytes)
}
