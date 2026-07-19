package routing

import (
	"reflect"
	"testing"

	"github.com/godexture/core/registry"
)

func TestAllocateResources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		requests []bool
		budget   int
		want     []int
	}{
		{name: "none", requests: []bool{false, false}, budget: 8, want: []int{0, 0}},
		{name: "one", requests: []bool{false, true, false}, budget: 8, want: []int{0, 8, 0}},
		{name: "even", requests: []bool{true, true}, budget: 8, want: []int{4, 4}},
		{name: "remainder", requests: []bool{true, false, true, true}, budget: 8, want: []int{3, 0, 3, 2}},
		{name: "more stages than budget", requests: []bool{true, true, true}, budget: 1, want: []int{1, 1, 1}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			requests := make([]registry.ResourceRequest, len(test.requests))
			for i, parallel := range test.requests {
				requests[i].Parallelism = parallel
			}
			allocations := allocateResources(requests, test.budget)
			got := make([]int, len(allocations))
			for i := range allocations {
				got[i] = allocations[i].Parallelism
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("allocateResources() = %v, want %v", got, test.want)
			}
		})
	}
}
