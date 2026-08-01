package config

import (
	"time"
)

type FadeConfig struct {
	FadeIn           time.Duration `name:"fade-in" check:"nonnegative" help:"Fade-in duration"`
	FadeOut          time.Duration `name:"fade-out" check:"nonnegative" help:"Fade-out duration"`
	MemoryLimitBytes int64         `name:"memory-limit-bytes" help:"Maximum buffered memory"`
	TempDir          string        `name:"temp-dir" help:"Temporary directory"`
}

var DefaultFadeConfig = FadeConfig{MemoryLimitBytes: defaultMemoryLimitBytes}

func (c FadeConfig) Validate() error {
	return validateMemoryLimit(c.MemoryLimitBytes)
}
