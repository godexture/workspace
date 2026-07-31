package config

import (
	"fmt"
)

type NormalizeConfig struct {
	TargetPeakDBFS     float64 `name:"target-peak-dbfs" check:"finite" help:"Target peak level"`
	AllowAmplification bool    `name:"allow-amplification" help:"Allow gain above unity"`
	MemoryLimitBytes   int64   `name:"memory-limit-bytes" help:"Maximum buffered memory"`
	TempDir            string  `name:"temp-dir" help:"Temporary directory"`
}

var DefaultNormalizeConfig = NormalizeConfig{
	TargetPeakDBFS:     -1,
	AllowAmplification: true,
	MemoryLimitBytes:   defaultMemoryLimitBytes,
}

func (c NormalizeConfig) Validate() error {
	if c.TargetPeakDBFS > 0 {
		return fmt.Errorf("target peak must be finite and no greater than 0 dBFS")
	}
	return validateMemoryLimit(c.MemoryLimitBytes)
}
