package flac

import (
	"runtime"
	"testing"
)

func TestResolveEngineOptionsDefaultsToGOMAXPROCS(t *testing.T) {
	t.Parallel()
	options, err := resolveEngineOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if options.parallelism != runtime.GOMAXPROCS(0) {
		t.Fatalf("parallelism = %d, want %d", options.parallelism, runtime.GOMAXPROCS(0))
	}
}

func TestResolveEngineOptionsAppliesParallelism(t *testing.T) {
	t.Parallel()
	options, err := resolveEngineOptions([]EngineOption{WithParallelism(3)})
	if err != nil {
		t.Fatal(err)
	}
	if options.parallelism != 3 {
		t.Fatalf("parallelism = %d, want 3", options.parallelism)
	}
}

func TestResolveEngineOptionsRejectsInvalidOptions(t *testing.T) {
	t.Parallel()
	for name, options := range map[string][]EngineOption{
		"nil":      {nil},
		"zero":     {WithParallelism(0)},
		"negative": {WithParallelism(-1)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := resolveEngineOptions(options); err == nil {
				t.Fatal("resolveEngineOptions() succeeded")
			}
		})
	}
}
