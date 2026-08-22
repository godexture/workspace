package run

import (
	"errors"
	"testing"
	"time"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
)

func TestExpandConnectionsPairsRepeatedDescriptorsAndProjectsOneLogicalBuffer(t *testing.T) {
	type timedID struct{}
	typ := schema.Define[timedID](schema.Traits[int]{Time: func(value int) (int64, bool) { return int64(value), true }})
	first := stream.MustDescriptor("first", typ.Descriptor(), timing.MustBase(1, 1_000), property.New())
	second := stream.MustDescriptor("second", typ.Descriptor(), timing.MustBase(1, 48_000), property.New())
	logical := job.Connect(job.At("source", "tracks"), job.At("sink", "tracks"))
	template := Template{
		nodes: []node{
			{id: "source", kind: drive.Source, binding: drive.NewSource("tracks", typ), outputs: flow.NewDescriptors(flow.Describe("tracks", first), flow.Describe("tracks", second))},
			{id: "sink", kind: drive.Sink, binding: drive.NewSink("tracks", typ), inputs: flow.NewDescriptors(flow.Describe("tracks", first), flow.Describe("tracks", second))},
		},
		edges:    []edge{{value: logical, from: 0, to: 1}},
		incoming: make([][]int, 2),
		outgoing: make([][]int, 2),
	}
	policy := job.QueuePolicy{Items: 2, Span: 250 * time.Millisecond}
	if err := template.expandConnections(policy); err != nil {
		t.Fatal(err)
	}
	if len(template.connections) != 2 {
		t.Fatalf("physical connections = %d", len(template.connections))
	}
	if firstConnection, secondConnection := template.connections[0], template.connections[1]; firstConnection.route != 0 || firstConnection.input != 0 || secondConnection.route != 1 || secondConnection.input != 1 || firstConnection.limit.Span != 250 || secondConnection.limit.Span != 12_000 {
		t.Fatalf("connections = %#v", template.connections)
	}
	template.placeBuffers()
	projection := template.project()
	if len(projection.Buffers) != 1 || projection.Buffers[0].ID != edgeKey(logical) || projection.Buffers[0].Limit.Span != 250*time.Millisecond {
		t.Fatalf("logical buffer projection = %#v", projection.Buffers)
	}
	slots, err := template.QueueSlots()
	if err != nil || slots != 4 {
		t.Fatalf("queue slots = %d, %v", slots, err)
	}
	multiplier, err := template.InFlightMultiplier("source")
	if err != nil || multiplier != 6 {
		t.Fatalf("in-flight multiplier = %d, %v", multiplier, err)
	}
}

func TestExpandConnectionsUsesTargetPortAndSourceOrder(t *testing.T) {
	type plainID struct{}
	typ := schema.Define[plainID](schema.Traits[int]{})
	first := stream.MustDescriptor("first", typ.Descriptor(), timing.Base{}, property.New())
	second := stream.MustDescriptor("second", typ.Descriptor(), timing.Base{}, property.New())
	template := Template{
		nodes: []node{
			{id: "a", outputs: flow.NewDescriptors(flow.Describe("out", first))},
			{id: "b", outputs: flow.NewDescriptors(flow.Describe("out", second))},
			{id: "target", inputs: flow.NewDescriptors(flow.Describe("in", first), flow.Describe("in", second))},
		},
		edges: []edge{
			{value: job.Connect(job.At("b", "out"), job.At("target", "in")), from: 1, to: 2},
			{value: job.Connect(job.At("a", "out"), job.At("target", "in")), from: 0, to: 2},
		},
		incoming: make([][]int, 3),
		outgoing: make([][]int, 3),
	}
	if err := template.expandConnections(job.QueuePolicy{Items: 1}); err != nil {
		t.Fatal(err)
	}
	if len(template.connections) != 2 || template.connections[0].logical != 1 || template.connections[0].input != 0 || template.connections[1].logical != 0 || template.connections[1].input != 1 {
		t.Fatalf("connection order = %#v", template.connections)
	}
}

func TestExpandConnectionsRejectsDescriptorMismatchAndCardinality(t *testing.T) {
	type plainID struct{}
	typ := schema.Define[plainID](schema.Traits[int]{})
	first := stream.MustDescriptor("first", typ.Descriptor(), timing.Base{}, property.New())
	second := stream.MustDescriptor("second", typ.Descriptor(), timing.Base{}, property.New())
	logical := job.Connect(job.At("source", "out"), job.At("sink", "in"))
	for name, inputs := range map[string]flow.Descriptors[stream.Descriptor]{
		"mismatch":    flow.NewDescriptors(flow.Describe("in", second), flow.Describe("in", first)),
		"cardinality": flow.NewDescriptors(flow.Describe("in", first)),
	} {
		t.Run(name, func(t *testing.T) {
			template := Template{
				nodes: []node{
					{id: "source", outputs: flow.NewDescriptors(flow.Describe("out", first), flow.Describe("out", second))},
					{id: "sink", inputs: inputs},
				},
				edges:    []edge{{value: logical, from: 0, to: 1}},
				incoming: make([][]int, 2),
				outgoing: make([][]int, 2),
			}
			if err := template.expandConnections(job.QueuePolicy{Items: 1}); !errors.Is(err, ErrTopology) {
				t.Fatalf("expand error = %v", err)
			}
		})
	}
}
