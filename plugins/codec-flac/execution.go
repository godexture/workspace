package flac

import (
	"fmt"
	"runtime"
)

type EngineOption interface {
	applyEngineOption(*engineOptions)
}

type engineOptions struct {
	parallelism int
}

type parallelismOption int

func (o parallelismOption) applyEngineOption(options *engineOptions) {
	options.parallelism = int(o)
}

// WithParallelism sets the execution parallelism without changing codec
// semantics or encoded output.
func WithParallelism(parallelism int) EngineOption {
	return parallelismOption(parallelism)
}

func resolveEngineOptions(options []EngineOption) (engineOptions, error) {
	resolved := engineOptions{parallelism: runtime.GOMAXPROCS(0)}
	for _, option := range options {
		if option == nil {
			return engineOptions{}, fmt.Errorf("FLAC engine option must not be nil")
		}
		option.applyEngineOption(&resolved)
	}
	if resolved.parallelism < 1 {
		return engineOptions{}, fmt.Errorf("FLAC parallelism must be positive: %d", resolved.parallelism)
	}
	return resolved, nil
}
