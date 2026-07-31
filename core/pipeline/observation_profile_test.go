package pipeline

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// TestPipelineObservationDoesNotLeakGoroutines is the M0 baseline for
// docs/refactor/quality.md's "observation off/on の ... goroutine profile"
// item: each observation mode (and the Plain, unobserved path) must return
// to the pre-run goroutine count after Run() completes, run after run.
func TestPipelineObservationDoesNotLeakGoroutines(t *testing.T) {
	const packets = 64
	const packetSize = 4096

	variants := []struct {
		name  string
		plain bool
		mode  ObservationMode
	}{
		{name: "Plain", plain: true},
		{name: "Off", mode: ObservationOff},
		{name: "Progress", mode: ObservationProgress},
		{name: "Metrics", mode: ObservationMetrics},
	}

	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			before := settledGoroutineCount(t)

			for range 5 {
				var conversion *Pipeline
				var err error
				if variant.plain {
					conversion, err = plainObservationPipeline(packets, packetSize)
				} else {
					conversion, _, err = observationPipeline(variant.mode, packets, packetSize, false)
				}
				if err != nil {
					t.Fatalf("build pipeline: %v", err)
				}
				if err := conversion.Run(context.Background()); err != nil {
					t.Fatalf("Run(): %v", err)
				}
			}

			after := settledGoroutineCount(t)
			if after > before {
				t.Fatalf("goroutine count grew from %d to %d across 5 runs", before, after)
			}
		})
	}
}

// settledGoroutineCount lets recently-finished goroutines unwind before
// sampling, so transient scheduler noise doesn't produce a false leak.
func settledGoroutineCount(t *testing.T) int {
	t.Helper()
	runtime.Gosched()
	time.Sleep(10 * time.Millisecond)
	runtime.GC()
	return runtime.NumGoroutine()
}
