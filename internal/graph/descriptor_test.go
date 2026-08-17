package graph

import (
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

func descriptorManyInputShape() flow.Shape {
	return flow.NewShape([]flow.Port{flow.In("tracks", graphSchemaA, flow.Many(), flow.WithFanIn(flow.ZipFanIn))}, nil)
}

func descriptorManyOutputShape() flow.Shape {
	return flow.NewShape(nil, []flow.Port{flow.Out("tracks", graphSchemaA, flow.Many())})
}

func descriptorBindingsFor(port string, ids ...stream.ID) flow.Descriptors[stream.Descriptor] {
	bindings := make([]flow.PortDescriptor[stream.Descriptor], 0, len(ids))
	for _, id := range ids {
		bindings = append(bindings, flow.Describe(port, fixtureDescriptor(id, graphSchemaA)))
	}
	return flow.NewDescriptors(bindings...)
}

func TestValidateDescriptorBindingsAcceptsOrderedMany(t *testing.T) {
	for _, test := range []struct {
		name      string
		direction flow.Direction
		ports     []flow.Port
	}{
		{name: "input", direction: flow.InputDirection, ports: descriptorManyInputShape().Inputs},
		{name: "output", direction: flow.OutputDirection, ports: descriptorManyOutputShape().Outputs},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := shapedNode{request: fixtureNode[graphSinkID]("node"), shape: flow.NewShape(test.ports, nil)}
			if test.direction == flow.OutputDirection {
				node.shape = flow.NewShape(nil, test.ports)
			}
			items := validateDescriptorBindings(node, test.direction, test.ports, descriptorBindingsFor("tracks", "track-a", "track-b").Bindings(), true)
			if len(items) != 0 {
				t.Fatalf("ordered many bindings rejected: %v", items)
			}
		})
	}
}

func TestCompilePreservesOrderedManyDescriptors(t *testing.T) {
	sourceCompileMany := func(flow.Descriptors[stream.Descriptor]) plugin.Compiled[graphPlan, stream.Descriptor] {
		return plugin.Compiled[graphPlan, stream.Descriptor]{Outputs: descriptorBindingsFor("tracks", "track-a", "track-b")}
	}
	index := fixtureCatalog(t,
		fixtureComponent[graphSourceID](descriptorManyOutputShape(), sourceCompileMany, nil, false),
		fixtureComponent[graphSinkID](descriptorManyInputShape(), sinkCompile, nil, false),
	)
	request := fixtureRequest(t,
		[]job.Node{fixtureNode[graphSourceID]("source"), fixtureNode[graphSinkID]("sink")},
		[]job.Edge{job.Connect(job.At("source", "tracks"), job.At("sink", "tracks"))},
	)
	compiled, err := Compile(index, request)
	if err != nil {
		t.Fatal(err)
	}
	sink, ok := compiled.Lookup("sink")
	if !ok {
		t.Fatal("compiled graph omitted sink")
	}
	values := sink.Inputs().At("tracks")
	if len(values) != 2 || values[0].ID() != "track-a" || values[1].ID() != "track-b" {
		t.Fatalf("ordered many input descriptors = %#v", values)
	}
}

func TestValidateDescriptorBindingsRejectsDuplicateStreamID(t *testing.T) {
	node := shapedNode{request: fixtureNode[graphSourceID]("source"), shape: descriptorManyOutputShape()}
	items := validateDescriptorBindings(node, flow.OutputDirection, node.shape.Outputs, descriptorBindingsFor("tracks", "track-a", "track-a").Bindings(), true)
	if !hasCode(items, "graph.duplicate-stream") {
		t.Fatalf("duplicate stream ID diagnostics = %v", items)
	}
}

func TestValidateDescriptorBindingsRejectsRepeatedOne(t *testing.T) {
	shape := sinkShape(graphSchemaA)
	node := shapedNode{request: fixtureNode[graphSinkID]("sink"), shape: shape}
	items := validateDescriptorBindings(node, flow.InputDirection, shape.Inputs, descriptorBindingsFor("in", "track-a", "track-b").Bindings(), true)
	if !hasCode(items, "graph.fan-in") {
		t.Fatalf("repeated One diagnostics = %v", items)
	}
}

func TestValidateDescriptorBindingsDefersRequiredGapCardinality(t *testing.T) {
	shape := descriptorManyInputShape()
	node := shapedNode{request: fixtureNode[graphSinkID]("sink"), shape: shape}
	items := validateDescriptorBindings(node, flow.InputDirection, shape.Inputs, nil, false)
	if hasCode(items, "graph.required-input") {
		t.Fatalf("deferred required input diagnostics = %v", items)
	}
	items = validateDescriptorBindings(node, flow.InputDirection, shape.Inputs, nil, true)
	if !hasCode(items, "graph.required-input") {
		t.Fatalf("complete required input diagnostics = %v", items)
	}
}

func TestValidateCompiledOutputsRejectsUnknownPort(t *testing.T) {
	node := shapedNode{request: fixtureNode[graphSourceID]("source"), shape: sourceShape(graphSchemaA)}
	items := validateCompiledOutputs(node, descriptorBindingsFor("missing", "track"), false)
	if !hasCode(items, "graph.unknown-output") {
		t.Fatalf("unknown output diagnostics = %v", items)
	}
}
