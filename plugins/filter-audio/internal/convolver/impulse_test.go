package convolver

import (
	"math"
	"reflect"
	"testing"

	"github.com/godexture/godec/core/registry"
	"github.com/godexture/godec/sdk/dsp/fft"
)

// TestBuildPartitionsWorkerCountDoesNotChangeOutput is the convolver's
// baseline for the M0 "worker 1/4/16 semantic diff" contract (see
// docs/refactor/performance.md): buildPartitions is the only place the
// engine hands work to a registry.WorkerPool, so pool size must not change
// the forward-transformed partitions it produces.
func TestBuildPartitionsWorkerCountDoesNotChangeOutput(t *testing.T) {
	const hop = 64
	plan, err := fft.NewRealPlan(2 * hop)
	if err != nil {
		t.Fatal(err)
	}
	samples := make([]float32, hop*10)
	for i := range samples {
		samples[i] = float32(math.Sin(float64(i) * 0.31))
	}

	want, err := buildPartitions(plan, hop, samples, nil)
	if err != nil {
		t.Fatalf("sequential buildPartitions() error = %v", err)
	}

	for _, workers := range []int{1, 4, 16} {
		pool := registry.NewWorkerPool(workers)
		t.Cleanup(func() { pool.Close() })

		got, err := buildPartitions(plan, hop, samples, pool)
		if err != nil {
			t.Fatalf("workers=%d: buildPartitions() error = %v", workers, err)
		}
		if len(got) != len(want) {
			t.Fatalf("workers=%d: partition count = %d, want %d", workers, len(got), len(want))
		}
		for i := range want {
			if !reflect.DeepEqual(got[i].spectrum, want[i].spectrum) {
				t.Fatalf("workers=%d: partition %d spectrum differs from the sequential build", workers, i)
			}
		}
	}
}
