package routing

import (
	"testing"

	"github.com/godexture/godec/core/registry"
)

func TestGrantResources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		requests []bool
		withPool bool
	}{
		{name: "none requested, pool available", requests: []bool{false, false}, withPool: true},
		{name: "one requested", requests: []bool{false, true, false}, withPool: true},
		{name: "all requested", requests: []bool{true, true}, withPool: true},
		{name: "mixed", requests: []bool{true, false, true, true}, withPool: true},
		{name: "no pool available", requests: []bool{true, true}, withPool: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			requests := make([]registry.ResourceRequest, len(test.requests))
			for i, parallel := range test.requests {
				requests[i].Parallelism = parallel
			}
			var pool *registry.WorkerPool
			if test.withPool {
				pool = registry.NewWorkerPool(4)
				defer pool.Close()
			}
			grants := grantResources(requests, pool)
			if len(grants) != len(requests) {
				t.Fatalf("len(grants) = %d, want %d", len(grants), len(requests))
			}
			for i, parallel := range test.requests {
				want := (*registry.WorkerPool)(nil)
				if parallel && test.withPool {
					want = pool
				}
				if grants[i].Pool != want {
					t.Fatalf("grants[%d].Pool = %v, want %v", i, grants[i].Pool, want)
				}
			}
		})
	}
}

func TestGrantResources_SharesOnePoolAcrossStages(t *testing.T) {
	t.Parallel()
	pool := registry.NewWorkerPool(4)
	defer pool.Close()
	requests := []registry.ResourceRequest{{Parallelism: true}, {Parallelism: false}, {Parallelism: true}}
	grants := grantResources(requests, pool)
	if grants[0].Pool == nil || grants[0].Pool != grants[2].Pool {
		t.Fatalf("parallel-eligible stages must share the same pool, got %v and %v", grants[0].Pool, grants[2].Pool)
	}
	if grants[1].Pool != nil {
		t.Fatalf("non-parallel stage got a pool: %v", grants[1].Pool)
	}
}
