package host

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type hostPlanPluginID struct{}
type hostPlanConfigID struct{}
type hostPlanSourceID struct{}
type hostPlanBridgeID struct{}
type hostPlanSinkID struct{}
type hostPlanSchemaAID struct{}
type hostPlanSchemaBID struct{}
type hostPlanUnit struct{}
type hostPlanConfig struct{}

var (
	hostPlanSchemaA = schema.Define[hostPlanSchemaAID, hostPlanUnit](schema.Traits[hostPlanUnit]{})
	hostPlanSchemaB = schema.Define[hostPlanSchemaBID, hostPlanUnit](schema.Traits[hostPlanUnit]{})
)

type hostPlanOperator struct{ shape flow.Shape }

func (o hostPlanOperator) Ports() flow.Shape { return o.shape.Clone() }
func (hostPlanOperator) Close() error        { return nil }

func hostPlanComponent[Marker any](shape flow.Shape, compile func(flow.Descriptors[stream.Descriptor]) plugin.Compiled[flow.Shape, stream.Descriptor], opened *atomic.Int32) plugin.Component {
	schemaValue := config.Struct[hostPlanConfigID](func() hostPlanConfig { return hostPlanConfig{} }).Version("1").Build()
	spec := plugin.Spec[hostPlanConfig, flow.Shape, stream.Descriptor]{
		Shape: plugin.StaticShape[hostPlanConfig](shape),
		Compile: func(plugin.CompileContext, hostPlanConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
			result := compile(flow.NewDescriptors[stream.Descriptor]())
			result.Plan = shape.Clone()
			return result, nil
		},
		Open: func(_ plugin.OpenContext, plan flow.Shape) (flow.Operator, error) {
			opened.Add(1)
			return hostPlanOperator{shape: plan}, nil
		},
	}
	if len(shape.Inputs) != 0 {
		spec.Compile = func(_ plugin.CompileContext, _ hostPlanConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
			result := compile(inputs)
			result.Plan = shape.Clone()
			return result, nil
		}
	}
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: "fixture"}, schemaValue, plugin.WithSpec(spec))
}

func TestHostPlanReturnsPublicPlanWithoutOpening(t *testing.T) {
	var opened atomic.Int32
	descriptorA := stream.MustDescriptor("stream", hostPlanSchemaA.Descriptor(), timing.Base{}, property.New())
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", hostPlanSchemaA)})
	source := hostPlanComponent[hostPlanSourceID](sourceShape, func(flow.Descriptors[stream.Descriptor]) plugin.Compiled[flow.Shape, stream.Descriptor] {
		return plugin.Compiled[flow.Shape, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", descriptorA))}
	}, &opened)
	bridgeShape := flow.NewShape([]flow.Port{flow.In("in", hostPlanSchemaA)}, []flow.Port{flow.Out("out", hostPlanSchemaB)})
	bridge := hostPlanComponent[hostPlanBridgeID](bridgeShape, func(inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[flow.Shape, stream.Descriptor] {
		input, _ := inputs.One("in")
		output := stream.MustDescriptor(input.ID(), hostPlanSchemaB.Descriptor(), input.TimeBase(), input.Properties()).WithMetadata(input.Metadata())
		return plugin.Compiled[flow.Shape, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", output)), Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "bridge"}}}
	}, &opened)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", hostPlanSchemaB)}, nil)
	sink := hostPlanComponent[hostPlanSinkID](sinkShape, func(flow.Descriptors[stream.Descriptor]) plugin.Compiled[flow.Shape, stream.Descriptor] {
		return plugin.Compiled[flow.Shape, stream.Descriptor]{Outputs: flow.NewDescriptors[stream.Descriptor]()}
	}, &opened)
	definition := plugin.Define[hostPlanPluginID](plugin.Descriptor{DisplayName: "planner", Version: "1"}, source, bridge, sink)
	host, err := New(Plugins(plugin.NewSet(definition)), PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}))
	if err != nil {
		t.Fatal(err)
	}
	requested, err := job.NewGraph(
		[]job.Node{
			job.NewNode("source", source.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, requested)
	if err != nil {
		t.Fatal(err)
	}
	public, err := host.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !public.Valid() || opened.Load() != 0 {
		t.Fatalf("Plan valid=%v opened=%d", public.Valid(), opened.Load())
	}
	automatic := 0
	for _, node := range public.Nodes() {
		if node.Origin == plan.Automatic && node.Component == bridge.Identity().String() {
			automatic++
		}
	}
	if automatic != 1 {
		t.Fatalf("automatic bridge count = %d", automatic)
	}
}

func TestHostPlanReportsMissingProvider(t *testing.T) {
	host, err := New()
	if err != nil {
		t.Fatal(err)
	}
	inputReference, _ := access.Parse("file:///input")
	outputReference, _ := access.Parse("file:///output")
	input, _ := job.InputFromReference(inputReference)
	output, _ := job.OutputToReference(outputReference)
	request, err := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = host.Plan(context.Background(), request)
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "bind.provider-not-found" {
		t.Fatalf("binding diagnostic = %#v", items)
	}
}
