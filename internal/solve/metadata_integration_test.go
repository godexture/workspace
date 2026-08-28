package solve

import (
	"context"
	"reflect"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/key"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type metadataIntegrationProducerID struct{}
type metadataIntegrationSinkAID struct{}
type metadataIntegrationSinkBID struct{}
type metadataIntegrationBridgeID struct{}
type metadataIntegrationCarrierID struct{}
type metadataIntegrationSourceCarrierID struct{}
type metadataIntegrationKeyID struct{}

func metadataIntegrationReport() loss.Report {
	return loss.Report{
		Carrier: carrier.Define[metadataIntegrationCarrierID](), Encoding: "fixture.output-encoding", Block: "fixture/output-block",
		Loss: loss.Loss{
			Key: key.Define[metadataIntegrationKeyID, string]().ID(), Kind: loss.Dropped, Native: "OUTN", Detail: "fixture.unrepresentable",
			Source: loss.Origin{Carrier: carrier.Define[metadataIntegrationSourceCarrierID](), Encoding: "fixture.source-encoding", Block: "fixture/source-block", Native: "SRCN"},
		},
	}
}

func metadataIntegrationProducer[Marker any](report loss.Report) plugin.Component {
	shape := flow.NewShape(nil, []flow.Port{flow.Out("out", solveSchemaA)})
	return solveComponent[Marker](shape, func(solveConfig, flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		return plugin.Compiled[solvePlan, stream.Descriptor]{
			Outputs:         flow.NewDescriptors(flow.Describe("out", solveDescriptor(solveSchemaA, 48_000))),
			MetadataReports: []plugin.MetadataReport{{Output: "out", Report: report}},
		}
	}, nil, 0, plugin.Contract{}, nil, nil)
}

func metadataIntegrationSink[Marker any]() plugin.Component {
	shape := flow.NewShape([]flow.Port{flow.In("in", solveSchemaA)}, nil)
	return solveComponent[Marker](shape, func(_ solveConfig, inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		if _, ok := inputs.One("in"); !ok {
			return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("fixture.input"))}}
		}
		return plugin.Compiled[solvePlan, stream.Descriptor]{Outputs: flow.NewDescriptors[stream.Descriptor]()}
	}, nil, 0, plugin.Contract{}, nil, nil)
}

func metadataIntegrationJob(t testing.TB, nodes []job.Node, edges []job.Edge, policy job.Policy) job.Job {
	t.Helper()
	requested, err := job.NewGraph(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, requested, job.WithPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func metadataIntegrationBoundary(choice int, node string, component plugin.Component) bound.Entry {
	return bound.Direct(plan.Boundary{
		Direction: plan.OutputBoundary, Kind: plan.DirectBoundary, Choice: choice,
		Node: node, Port: "in", Component: component.Identity().String(), Ownership: access.Owned,
	}, struct{}{}, func() error { return nil })
}

func TestResolveBoundProjectsMetadataReportToTerminalBoundary(t *testing.T) {
	producer := metadataIntegrationProducer[metadataIntegrationProducerID](metadataIntegrationReport())
	sink := metadataIntegrationSink[metadataIntegrationSinkAID]()
	policy, _ := job.PolicyFor(job.Fast)
	request := metadataIntegrationJob(t,
		[]job.Node{job.NewNode("producer", producer.Identity(), config.NewPatch()), job.NewNode("terminal", sink.Identity(), config.NewPatch())},
		[]job.Edge{job.Connect(job.At("producer", "out"), job.At("terminal", "in"))}, policy,
	)
	program, err := ResolveBound(context.Background(), solveIndex(t, producer, sink), request, solvePlatform(), bound.New(metadataIntegrationBoundary(0, "terminal", sink)), graph.CompileContexts{})
	if err != nil {
		t.Fatal(err)
	}
	want := plan.PredictedMetadataLoss{Output: 0, Node: "producer", Component: producer.Identity().String(), Port: "out", Report: metadataIntegrationReport()}
	if got := program.Plan().PredictedMetadataLosses(); !reflect.DeepEqual(got, []plan.PredictedMetadataLoss{want}) {
		t.Fatalf("projected metadata losses = %#v, want %#v", got, []plan.PredictedMetadataLoss{want})
	}
	if warnings := program.Plan().Warnings(); len(warnings) != 1 || warnings[0] != "metadata loss is predicted" {
		t.Fatalf("plan warnings = %#v", warnings)
	}

	strict := policy
	strict.Metadata = job.StrictMetadata
	strictRequest := metadataIntegrationJob(t,
		[]job.Node{job.NewNode("producer", producer.Identity(), config.NewPatch()), job.NewNode("terminal", sink.Identity(), config.NewPatch())},
		[]job.Edge{job.Connect(job.At("producer", "out"), job.At("terminal", "in"))}, strict,
	)
	_, err = ResolveBound(context.Background(), solveIndex(t, producer, sink), strictRequest, solvePlatform(), bound.New(metadataIntegrationBoundary(0, "terminal", sink)), graph.CompileContexts{})
	assertSolveCode(t, err, "solve.metadata-loss")
}

func TestResolveBoundFansOutMetadataReportToEachTerminalBoundary(t *testing.T) {
	producer := metadataIntegrationProducer[metadataIntegrationProducerID](metadataIntegrationReport())
	first := metadataIntegrationSink[metadataIntegrationSinkAID]()
	second := metadataIntegrationSink[metadataIntegrationSinkBID]()
	policy, _ := job.PolicyFor(job.Fast)
	request := metadataIntegrationJob(t,
		[]job.Node{
			job.NewNode("producer", producer.Identity(), config.NewPatch()),
			job.NewNode("first", first.Identity(), config.NewPatch()),
			job.NewNode("second", second.Identity(), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("producer", "out"), job.At("first", "in")),
			job.Connect(job.At("producer", "out"), job.At("second", "in")),
		}, policy,
	)
	program, err := ResolveBound(context.Background(), solveIndex(t, producer, first, second), request, solvePlatform(), bound.New(
		metadataIntegrationBoundary(0, "first", first),
		metadataIntegrationBoundary(1, "second", second),
	), graph.CompileContexts{})
	if err != nil {
		t.Fatal(err)
	}
	want := []plan.PredictedMetadataLoss{
		{Output: 0, Node: "producer", Component: producer.Identity().String(), Port: "out", Report: metadataIntegrationReport()},
		{Output: 1, Node: "producer", Component: producer.Identity().String(), Port: "out", Report: metadataIntegrationReport()},
	}
	if got := program.Plan().PredictedMetadataLosses(); !reflect.DeepEqual(got, want) {
		t.Fatalf("fan-out metadata losses = %#v, want %#v", got, want)
	}
}

func TestResolveBoundRejectsMetadataReportAcrossComponent(t *testing.T) {
	producer := metadataIntegrationProducer[metadataIntegrationProducerID](metadataIntegrationReport())
	bridge := solveBridge[metadataIntegrationBridgeID](solveSchemaA, solveSchemaA, plugin.Effect{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "fixture.bridge"}, identityTransform, nil, 0, plugin.Contract{}, nil, nil)
	sink := metadataIntegrationSink[metadataIntegrationSinkAID]()
	policy, _ := job.PolicyFor(job.Fast)
	request := metadataIntegrationJob(t,
		[]job.Node{
			job.NewNode("producer", producer.Identity(), config.NewPatch()),
			job.NewNode("bridge", bridge.Identity(), config.NewPatch()),
			job.NewNode("terminal", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("producer", "out"), job.At("bridge", "in")),
			job.Connect(job.At("bridge", "out"), job.At("terminal", "in")),
		}, policy,
	)
	_, err := ResolveBound(context.Background(), solveIndex(t, producer, bridge, sink), request, solvePlatform(), bound.New(metadataIntegrationBoundary(0, "terminal", sink)), graph.CompileContexts{})
	assertSolveCode(t, err, "solve.metadata-report-boundary")
}
