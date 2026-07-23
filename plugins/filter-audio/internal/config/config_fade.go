package config

import (
	"fmt"
	"time"
)

type FadeConfig struct {
	FadeIn           time.Duration `name:"fade-in" help:"Fade-in duration"`
	FadeOut          time.Duration `name:"fade-out" help:"Fade-out duration"`
	MemoryLimitBytes int64         `name:"memory-limit-bytes" help:"Maximum buffered memory"`
	TempDir          string        `name:"temp-dir" help:"Temporary directory"`
}

var DefaultFadeConfig = FadeConfig{MemoryLimitBytes: defaultMemoryLimitBytes}

func (c FadeConfig) Validate() error {
	if c.FadeIn < 0 || c.FadeOut < 0 {
		return fmt.Errorf("fade durations must not be negative")
	}
	return validateMemoryLimit(c.MemoryLimitBytes)
}
