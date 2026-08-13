package program

import (
	"math"
	"testing"

	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/resource"
)

func TestInFlightMultiplierCountsReachableHoldersAndPhysicalQueues(t *testing.T) {
	edges := []plan.Edge{
		{FromNode: "source", ToNode: "middle"},
		{FromNode: "middle", ToNode: "sink"},
		{FromNode: "middle", ToNode: "branch"},
		{FromNode: "other", ToNode: "other-sink"},
	}
	runtime := plan.Runtime{Buffers: []plan.Buffer{
		{FromNode: "middle", Limit: plan.Limit{Items: 4}},
		{FromNode: "branch", Limit: plan.Limit{Items: 2}},
		{FromNode: "other", Limit: plan.Limit{Items: 8}},
	}}
	tests := map[string]struct {
		node string
		want uint64
	}{
		// source, middle, sink, branch hold one each; queues add 4+2.
		"upstream": {node: "source", want: 10},
		// middle, sink, branch hold one each; queues add 4+2.
		"middle": {node: "middle", want: 9},
		// A terminal node holds only the item it is working on.
		"after queue": {node: "sink", want: 1},
		// other and other-sink hold one each; the queue adds 8.
		"other branch": {node: "other", want: 10},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := inFlightMultiplier(test.node, edges, runtime)
			if err != nil || got != test.want {
				t.Fatalf("multiplier = %d, %v; want %d", got, err, test.want)
			}
		})
	}
}

func TestPayloadMemoryScalingRejectsOverflow(t *testing.T) {
	if _, err := scaleMemory(resource.Bytes(math.MaxUint64), 2); err == nil {
		t.Fatal("payload memory overflow was accepted")
	}
}
