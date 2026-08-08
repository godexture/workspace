package graph

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type graphPluginID struct{}
type graphConfigID struct{}
type graphSourceID struct{}
type graphSecondSourceID struct{}
type graphTransformID struct{}
type graphSinkID struct{}
type graphSecondSinkID struct{}
type graphCycleAID struct{}
type graphCycleBID struct{}
type graphSchemaAID struct{}
type graphSchemaBID struct{}
type graphUnit struct{}
type graphOtherUnit struct{}
type graphConfig struct{}

var (
	graphSchemaA = schema.Define[graphSchemaAID, graphUnit](schema.Traits[graphUnit]{})
	graphSchemaB = schema.Define[graphSchemaBID, graphUnit](schema.Traits[graphUnit]{})
)

type graphPlan struct{ shape flow.Shape }
type graphOperator struct{ shape flow.Shape }

func (o graphOperator) Ports() flow.Shape { return o.shape.Clone() }
func (graphOperator) Close() error        { return nil }

type graphCompile func(flow.Descriptors[stream.Descriptor]) plugin.Compiled[graphPlan, stream.Descriptor]

func fixtureComponent[Marker any](shape flow.Shape, compile graphCompile, opened *atomic.Int32, finalizes bool) plugin.Component {
	schemaValue := config.Struct[graphConfigID](func() graphConfig { return graphConfig{} }).Version("1").Build()
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: "fixture"}, schemaValue, plugin.WithSpec(plugin.Spec[graphConfig, graphPlan, stream.Descriptor]{
		Shape: plugin.StaticShape[graphConfig](shape),
		Compile: func(_ plugin.CompileContext, _ graphConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[graphPlan, stream.Descriptor], error) {
			result := compile(inputs)
			result.Plan = graphPlan{shape: shape.Clone()}
			return result, nil
		},
		Open: func(_ plugin.OpenContext, plan graphPlan) (flow.Operator, error) {
			if opened != nil {
				opened.Add(1)
			}
			return graphOperator{shape: plan.shape.Clone()}, nil
		},
		Finalizes: finalizes,
	}))
}

func sourceShape(typ schema.Type[graphUnit], options ...flow.PortOption) flow.Shape {
	return flow.NewShape(nil, []flow.Port{flow.Out("out", typ, options...)})
}

func transformShape(typ schema.Type[graphUnit]) flow.Shape {
	return flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
}

func sinkShape(typ schema.Type[graphUnit], options ...flow.PortOption) flow.Shape {
	return flow.NewShape([]flow.Port{flow.In("in", typ, options...)}, nil)
}

func sourceCompile(typ schema.Type[graphUnit]) graphCompile {
	return func(flow.Descriptors[stream.Descriptor]) plugin.Compiled[graphPlan, stream.Descriptor] {
		return plugin.Compiled[graphPlan, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", fixtureDescriptor("stream", typ)))}
	}
}

func passCompile(inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[graphPlan, stream.Descriptor] {
	input, ok := inputs.One("in")
	if !ok {
		return plugin.Compiled[graphPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("fixture.input"))}}
	}
	return plugin.Compiled[graphPlan, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", input))}
}

func sinkCompile(flow.Descriptors[stream.Descriptor]) plugin.Compiled[graphPlan, stream.Descriptor] {
	return plugin.Compiled[graphPlan, stream.Descriptor]{Outputs: flow.NewDescriptors[stream.Descriptor]()}
}

func fixtureDescriptor(id stream.ID, typ schema.Type[graphUnit]) stream.Descriptor {
	return stream.MustDescriptor(id, typ.Identity(), timing.MustBase(1, 1000), property.New())
}

func fixtureCatalog(t *testing.T, components ...plugin.Component) catalog.Index {
	t.Helper()
	index, err := catalog.Build(plugin.NewSet(plugin.Define[graphPluginID](plugin.Descriptor{DisplayName: "graph fixture", Version: "1"}, components...)))
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func fixtureRequest(t *testing.T, nodes []job.Node, edges []job.Edge) job.Graph {
	t.Helper()
	request, err := job.NewGraph(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func fixtureNode[Marker any](id job.NodeID) job.Node {
	return job.NewNode(id, plugin.IdentityOf[Marker](), config.NewPatch())
}

func TestCompileBuildsTopologicalGraphWithoutOpeningOperators(t *testing.T) {
	var opened atomic.Int32
	index := fixtureCatalog(t,
		fixtureComponent[graphSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), &opened, false),
		fixtureComponent[graphTransformID](transformShape(graphSchemaA), passCompile, &opened, false),
		fixtureComponent[graphSinkID](sinkShape(graphSchemaA), sinkCompile, &opened, false),
	)
	request := fixtureRequest(t,
		[]job.Node{fixtureNode[graphSinkID]("sink"), fixtureNode[graphSourceID]("source"), fixtureNode[graphTransformID]("transform")},
		[]job.Edge{
			job.Connect(job.At("transform", "out"), job.At("sink", "in")),
			job.Connect(job.At("source", "out"), job.At("transform", "in")),
		},
	)

	compiled, err := Compile(index, request)
	if err != nil {
		t.Fatal(err)
	}
	if !compiled.Valid() || opened.Load() != 0 {
		t.Fatalf("compiled graph valid=%v open count=%d", compiled.Valid(), opened.Load())
	}
	nodes := compiled.Nodes()
	if got := []job.NodeID{nodes[0].ID(), nodes[1].ID(), nodes[2].ID()}; !reflect.DeepEqual(got, []job.NodeID{"source", "transform", "sink"}) {
		t.Fatalf("node order = %v", got)
	}
	output, ok := nodes[1].Outputs().One("out")
	if !ok || output.ID() != "stream" || output.Schema() != graphSchemaA.Identity() {
		t.Fatalf("transform output = %#v", output)
	}
	operator, err := compiled.Open(plugin.NewOpenContext(context.Background(), plugin.OpenServices{}), "transform")
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	if opened.Load() != 1 {
		t.Fatalf("Open count = %d", opened.Load())
	}
}

func TestEvaluateReturnsTypedSchemaGapWithoutOpeningOperators(t *testing.T) {
	var opened atomic.Int32
	index := fixtureCatalog(t,
		fixtureComponent[graphSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), &opened, false),
		fixtureComponent[graphSinkID](sinkShape(graphSchemaB), sinkCompile, &opened, false),
	)
	request := fixtureRequest(t,
		[]job.Node{fixtureNode[graphSourceID]("source"), fixtureNode[graphSinkID]("sink")},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
	)
	evaluation, err := Evaluate(index, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, complete := evaluation.Graph(); complete {
		t.Fatal("schema-mismatched graph evaluated as complete")
	}
	gaps := evaluation.Gaps()
	if len(gaps) != 1 || gaps[0].Node() != "sink" || gaps[0].Port() != "in" || gaps[0].Need().Code() != "graph.schema-mismatch" {
		t.Fatalf("gaps = %#v", gaps)
	}
	edge, edgeOK := gaps[0].Edge()
	input, inputOK := gaps[0].Input()
	desired, desiredOK := gaps[0].Need().Desired()
	if !edgeOK || edge.From() != job.At("source", "out") || !inputOK || input.Schema() != graphSchemaA.Identity() || !desiredOK || desired.Schema() != graphSchemaB.Identity() {
		t.Fatalf("gap edge=%#v input=%#v desired=%#v", edge, input, desired)
	}
	accepted, err := gaps[0].Accepts(desired)
	if err != nil || !accepted {
		t.Fatalf("desired descriptor accepted=%v error=%v", accepted, err)
	}
	if accepted, err := gaps[0].Accepts(input); err != nil || accepted {
		t.Fatalf("mismatched descriptor accepted=%v error=%v", accepted, err)
	}
	if opened.Load() != 0 {
		t.Fatalf("evaluation opened %d operators", opened.Load())
	}
}

func TestEvaluateConfirmsConditionGapThroughDownstreamCompile(t *testing.T) {
	requireAccepted := func(inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[graphPlan, stream.Descriptor] {
		input, ok := inputs.One("in")
		if !ok || input.ID() != "accepted" {
			return plugin.Compiled[graphPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("fixture.accepted"))}}
		}
		return plugin.Compiled[graphPlan, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", input))}
	}
	index := fixtureCatalog(t,
		fixtureComponent[graphSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), nil, false),
		fixtureComponent[graphTransformID](transformShape(graphSchemaA), requireAccepted, nil, false),
		fixtureComponent[graphSinkID](sinkShape(graphSchemaA), sinkCompile, nil, false),
	)
	request := fixtureRequest(t,
		[]job.Node{fixtureNode[graphSourceID]("source"), fixtureNode[graphTransformID]("transform"), fixtureNode[graphSinkID]("sink")},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("transform", "in")),
			job.Connect(job.At("transform", "out"), job.At("sink", "in")),
		},
	)
	evaluation, err := Evaluate(index, request)
	if err != nil {
		t.Fatal(err)
	}
	gaps := evaluation.Gaps()
	if len(gaps) != 1 || gaps[0].Need().Code() != "fixture.accepted" {
		t.Fatalf("condition gaps = %#v", gaps)
	}
	original, _ := gaps[0].Input()
	if accepted, err := gaps[0].Accepts(original); err != nil || accepted {
		t.Fatalf("original descriptor accepted=%v error=%v", accepted, err)
	}
	candidate := stream.MustDescriptor("accepted", graphSchemaA.Identity(), original.TimeBase(), original.Properties()).WithMetadata(original.Metadata())
	if accepted, err := gaps[0].Accepts(candidate); err != nil || !accepted {
		t.Fatalf("condition candidate accepted=%v error=%v", accepted, err)
	}
}

func TestCompileRejectsTopologyFailuresWithStableCodes(t *testing.T) {
	tests := []struct {
		name       string
		components []plugin.Component
		nodes      []job.Node
		edges      []job.Edge
		codes      []string
	}{
		{
			name: "schema mismatch",
			components: []plugin.Component{
				fixtureComponent[graphSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), nil, false),
				fixtureComponent[graphSinkID](sinkShape(graphSchemaB), sinkCompile, nil, false),
			},
			nodes: []job.Node{fixtureNode[graphSourceID]("source"), fixtureNode[graphSinkID]("sink")},
			edges: []job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
			codes: []string{"graph.schema-mismatch"},
		},
		{
			name: "unknown ports",
			components: []plugin.Component{
				fixtureComponent[graphSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), nil, false),
				fixtureComponent[graphSinkID](sinkShape(graphSchemaA), sinkCompile, nil, false),
			},
			nodes: []job.Node{fixtureNode[graphSourceID]("source"), fixtureNode[graphSinkID]("sink")},
			edges: []job.Edge{job.Connect(job.At("source", "missing"), job.At("sink", "absent"))},
			codes: []string{"graph.unknown-output", "graph.unknown-input", "graph.required-output", "graph.required-input"},
		},
		{
			name: "fan in",
			components: []plugin.Component{
				fixtureComponent[graphSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), nil, false),
				fixtureComponent[graphSecondSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), nil, false),
				fixtureComponent[graphSinkID](sinkShape(graphSchemaA), sinkCompile, nil, false),
			},
			nodes: []job.Node{fixtureNode[graphSourceID]("a"), fixtureNode[graphSecondSourceID]("b"), fixtureNode[graphSinkID]("sink")},
			edges: []job.Edge{
				job.Connect(job.At("a", "out"), job.At("sink", "in")),
				job.Connect(job.At("b", "out"), job.At("sink", "in")),
			},
			codes: []string{"graph.fan-in"},
		},
		{
			name: "fan out",
			components: []plugin.Component{
				fixtureComponent[graphSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), nil, false),
				fixtureComponent[graphSinkID](sinkShape(graphSchemaA), sinkCompile, nil, false),
				fixtureComponent[graphSecondSinkID](sinkShape(graphSchemaA), sinkCompile, nil, false),
			},
			nodes: []job.Node{fixtureNode[graphSourceID]("source"), fixtureNode[graphSinkID]("a"), fixtureNode[graphSecondSinkID]("b")},
			edges: []job.Edge{
				job.Connect(job.At("source", "out"), job.At("a", "in")),
				job.Connect(job.At("source", "out"), job.At("b", "in")),
			},
			codes: []string{"graph.fan-out"},
		},
		{
			name: "required and reachability",
			components: []plugin.Component{
				fixtureComponent[graphSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), nil, false),
				fixtureComponent[graphSinkID](sinkShape(graphSchemaA), sinkCompile, nil, false),
			},
			nodes: []job.Node{fixtureNode[graphSourceID]("source"), fixtureNode[graphSinkID]("sink")},
			codes: []string{"graph.required-output", "graph.required-input", "graph.no-sink-path", "graph.unreachable"},
		},
		{
			name: "cycle",
			components: []plugin.Component{
				fixtureComponent[graphCycleAID](transformShape(graphSchemaA), passCompile, nil, false),
				fixtureComponent[graphCycleBID](transformShape(graphSchemaA), passCompile, nil, false),
			},
			nodes: []job.Node{fixtureNode[graphCycleAID]("a"), fixtureNode[graphCycleBID]("b")},
			edges: []job.Edge{
				job.Connect(job.At("a", "out"), job.At("b", "in")),
				job.Connect(job.At("b", "out"), job.At("a", "in")),
			},
			codes: []string{"graph.cycle", "graph.no-source", "graph.no-sink"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Compile(fixtureCatalog(t, test.components...), fixtureRequest(t, test.nodes, test.edges))
			assertCodes(t, err, test.codes...)
		})
	}
}

func TestCompileRejectsSemanticComponentFailures(t *testing.T) {
	tests := []struct {
		name    string
		compile graphCompile
		code    string
	}{
		{
			name: "invalid descriptor and time base",
			compile: func(flow.Descriptors[stream.Descriptor]) plugin.Compiled[graphPlan, stream.Descriptor] {
				return plugin.Compiled[graphPlan, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", stream.Descriptor{}))}
			},
			code: "graph.time-base",
		},
		{
			name: "unresolved requirement",
			compile: func(flow.Descriptors[stream.Descriptor]) plugin.Compiled[graphPlan, stream.Descriptor] {
				return plugin.Compiled[graphPlan, stream.Descriptor]{
					Outputs:      flow.NewDescriptors[stream.Descriptor](),
					Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("fixture.unmet"))},
				}
			},
			code: "graph.requirement",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var components []plugin.Component
			var nodes []job.Node
			var edges []job.Edge
			if test.name == "invalid descriptor and time base" {
				components = []plugin.Component{
					fixtureComponent[graphSourceID](sourceShape(graphSchemaA), test.compile, nil, false),
					fixtureComponent[graphSinkID](sinkShape(graphSchemaA), sinkCompile, nil, false),
				}
				nodes = []job.Node{fixtureNode[graphSourceID]("source"), fixtureNode[graphSinkID]("sink")}
				edges = []job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))}
			} else {
				components = []plugin.Component{
					fixtureComponent[graphSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), nil, false),
					fixtureComponent[graphTransformID](transformShape(graphSchemaA), test.compile, nil, false),
					fixtureComponent[graphSinkID](sinkShape(graphSchemaA), sinkCompile, nil, false),
				}
				nodes = []job.Node{fixtureNode[graphSourceID]("source"), fixtureNode[graphTransformID]("transform"), fixtureNode[graphSinkID]("sink")}
				edges = []job.Edge{
					job.Connect(job.At("source", "out"), job.At("transform", "in")),
					job.Connect(job.At("transform", "out"), job.At("sink", "in")),
				}
			}
			_, err := Compile(fixtureCatalog(t, components...), fixtureRequest(t, nodes, edges))
			assertCodes(t, err, test.code)
		})
	}
}

func TestCompileRejectsMissingFinalizerCapability(t *testing.T) {
	requiresFinalizer := func(flow.Descriptors[stream.Descriptor]) plugin.Compiled[graphPlan, stream.Descriptor] {
		return plugin.Compiled[graphPlan, stream.Descriptor]{
			Outputs:      flow.NewDescriptors(flow.Describe("out", fixtureDescriptor("stream", graphSchemaA))),
			Finalization: plugin.RequiresFinalization,
		}
	}
	index := fixtureCatalog(t,
		fixtureComponent[graphSourceID](sourceShape(graphSchemaA), requiresFinalizer, nil, false),
		fixtureComponent[graphSinkID](sinkShape(graphSchemaA), sinkCompile, nil, false),
	)
	request := fixtureRequest(t,
		[]job.Node{fixtureNode[graphSourceID]("source"), fixtureNode[graphSinkID]("sink")},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
	)
	_, err := Compile(index, request)
	assertCodes(t, err, "plugin.finalizer")
}

func TestTopologyDiagnosticsAreCanonical(t *testing.T) {
	index := fixtureCatalog(t,
		fixtureComponent[graphSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), nil, false),
		fixtureComponent[graphSecondSourceID](sourceShape(graphSchemaA), sourceCompile(graphSchemaA), nil, false),
		fixtureComponent[graphSinkID](sinkShape(graphSchemaA), sinkCompile, nil, false),
	)
	nodes := []job.Node{fixtureNode[graphSourceID]("b"), fixtureNode[graphSecondSourceID]("a"), fixtureNode[graphSinkID]("sink")}
	edges := []job.Edge{
		job.Connect(job.At("b", "out"), job.At("sink", "in")),
		job.Connect(job.At("a", "out"), job.At("sink", "in")),
	}
	_, firstErr := Compile(index, fixtureRequest(t, nodes, edges))
	_, secondErr := Compile(index, fixtureRequest(t, []job.Node{nodes[2], nodes[1], nodes[0]}, []job.Edge{edges[1], edges[0]}))
	if !reflect.DeepEqual(diagnostic.ItemsOf(firstErr), diagnostic.ItemsOf(secondErr)) {
		t.Fatalf("diagnostic order depends on request order:\nfirst:  %v\nsecond: %v", firstErr, secondErr)
	}
}

func TestTopologyRejectsSameSchemaMarkerWithDifferentPayloadTypes(t *testing.T) {
	conflicting := schema.Define[graphSchemaAID, graphOtherUnit](schema.Traits[graphOtherUnit]{})
	nodes := []shapedNode{
		{request: fixtureNode[graphSourceID]("source"), shape: sourceShape(graphSchemaA)},
		{request: fixtureNode[graphSinkID]("sink"), shape: flow.NewShape([]flow.Port{flow.In("in", conflicting)}, nil)},
	}
	edges := []job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))}
	_, items := validateTopology(nodes, edges)
	if !hasCode(items, "graph.schema-mismatch") {
		t.Fatalf("schema payload mismatch diagnostics = %v", items)
	}
}

func assertCodes(t *testing.T, err error, expected ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected diagnostics %v, got nil", expected)
	}
	codes := make(map[string]bool)
	for _, item := range diagnostic.ItemsOf(err) {
		codes[item.Code] = true
	}
	for _, code := range expected {
		if !codes[code] {
			t.Errorf("missing diagnostic %q in %v", code, err)
		}
	}
}

func hasCode(items []diagnostic.Item, expected string) bool {
	for _, item := range items {
		if item.Code == expected {
			return true
		}
	}
	return false
}
