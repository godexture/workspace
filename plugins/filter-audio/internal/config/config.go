package config

//go:generate enum-generator

import (
	"fmt"
	"math"
)

const defaultMemoryLimitBytes int64 = 64 << 20

func validateMemoryLimit(value int64) error {
	if value <= 0 {
		return fmt.Errorf("memory limit must be positive: %d", value)
	}
	return nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
