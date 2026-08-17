package run

import (
	"math"
	"testing"

	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
)

func TestInFlightMultiplierCountsReachableHoldersAndPhysicalQueues(t *testing.T) {
	template := resourceTemplate()
	tests := map[string]struct {
		node job.NodeID
		want uint64
	}{
		"upstream":     {node: "source", want: 10},
		"middle":       {node: "middle", want: 9},
		"terminal":     {node: "sink", want: 1},
		"other branch": {node: "other", want: 10},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := template.InFlightMultiplier(test.node)
			if err != nil || got != test.want {
				t.Fatalf("multiplier = %d, %v; want %d", got, err, test.want)
			}
		})
	}
	if _, err := template.InFlightMultiplier("absent"); err == nil {
		t.Fatal("unknown node multiplier was accepted")
	}
}

func TestQueueSlotsCountsPhysicalConnectionsAndRejectsOverflow(t *testing.T) {
	template := Template{connections: []connection{
		{reason: plan.SourceBuffer, limit: queueLimit(2)},
		{reason: plan.SourceBuffer, limit: queueLimit(2)},
	}}
	slots, err := template.QueueSlots()
	if err != nil || slots != 4 {
		t.Fatalf("physical slots = %d, %v", slots, err)
	}
	overflow := Template{connections: []connection{
		{reason: plan.SourceBuffer, limit: queueLimit(math.MaxUint32)},
		{reason: plan.SinkBuffer, limit: queueLimit(1)},
	}}
	if _, err := overflow.QueueSlots(); err == nil {
		t.Fatal("queue slot overflow was accepted")
	}
}

func resourceTemplate() Template {
	return Template{
		nodes: []node{
			{id: "source"},
			{id: "middle"},
			{id: "sink"},
			{id: "branch"},
			{id: "other"},
			{id: "other-sink"},
		},
		connections: []connection{
			{from: 0, to: 1},
			{from: 1, to: 2, reason: plan.SourceBuffer, limit: queueLimit(4)},
			{from: 1, to: 3, reason: plan.SourceBuffer, limit: queueLimit(2)},
			{from: 4, to: 5, reason: plan.SourceBuffer, limit: queueLimit(8)},
		},
		outgoing: [][]int{
			{0},
			{1, 2},
			nil,
			nil,
			{3},
			nil,
		},
	}
}

func queueLimit(items int) queue.Limit { return queue.Limit{Items: items} }
