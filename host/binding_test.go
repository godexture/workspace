package host

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type (
	boundaryPluginID    struct{}
	boundaryConfigID    struct{}
	boundarySchemaID    struct{}
	boundarySourceID    struct{}
	boundaryTransformID struct{}
	boundarySinkID      struct{}
)

type boundaryConfig struct{}
type boundaryUnit struct{}
type boundaryPlan struct{ shape flow.Shape }

var boundarySchema = schema.Define[boundarySchemaID](schema.Traits[boundaryUnit]{})

type boundaryOperator struct{ shape flow.Shape }

func (o boundaryOperator) Ports() flow.Shape { return o.shape.Clone() }
func (boundaryOperator) Close() error        { return nil }

func TestHostBindsProviderAndEndpointWithoutOpening(t *testing.T) {
	var opens atomic.Int32
	source, transform, sink, descriptor := boundaryComponents(&opens)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, transform, sink))
	capabilities, err := access.NewCapabilities(access.RandomRead, access.SequentialRead, access.StableSize)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := access.DefineProvider[boundarySourceID](
		[]string{"memory"},
		access.WithProviderCapabilities(capabilities),
		access.WithProviderRequirements(access.NewRequirements(
			access.AnyOf(access.SequentialRead),
			access.AnyOf(access.RandomRead, access.StableSize),
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	trait, err := endpoint.NewTrait(endpoint.FiniteStatic, endpoint.Offline)
	if err != nil {
		t.Fatal(err)
	}
	sinkEndpoint, err := endpoint.New(sink, trait)
	if err != nil {
		t.Fatal(err)
	}
	host, err := New(
		Plugins(set),
		Providers(provider),
		Endpoints(sinkEndpoint),
		PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	reference, err := access.Parse("memory://user:secret@example/input?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	input, _ := job.InputFromReference(reference)
	endpointRequest, _ := job.NewEndpoint(sink.Identity(), config.NewPatch())
	output, _ := job.OutputToEndpoint(endpointRequest)
	request, err := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
	if err != nil {
		t.Fatal(err)
	}
	planned, err := host.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if opens.Load() != 0 {
		t.Fatalf("binding or planning opened %d component(s)", opens.Load())
	}
	if len(planned.Nodes()) != 2 || len(planned.Edges()) != 1 || len(planned.Boundaries()) != 2 {
		t.Fatalf("bound Plan = nodes %d, edges %d, boundaries %#v", len(planned.Nodes()), len(planned.Edges()), planned.Boundaries())
	}
	bindings := planned.Boundaries()
	inputBinding, outputBinding := bindings[0], bindings[1]
	if inputBinding.Direction != plan.InputBoundary || inputBinding.Kind != plan.ProviderBoundary || inputBinding.Component != source.Identity().String() {
		t.Fatalf("input binding = %#v", inputBinding)
	}
	if strings.Contains(inputBinding.Reference, "secret") || inputBinding.ReferenceFingerprint != reference.Fingerprint().String() {
		t.Fatalf("reference projection = %#v", inputBinding)
	}
	if len(inputBinding.Available) != 3 || len(inputBinding.Selected) != 1 || inputBinding.Selected[0] != access.SequentialRead {
		t.Fatalf("capability projection = available %v, selected %v", inputBinding.Available, inputBinding.Selected)
	}
	if outputBinding.Direction != plan.OutputBoundary || outputBinding.Kind != plan.EndpointBoundary || outputBinding.Topology != endpoint.FiniteStatic || outputBinding.Mode != endpoint.Offline {
		t.Fatalf("output binding = %#v", outputBinding)
	}
	for _, node := range planned.Nodes() {
		for _, port := range append(append([]plan.PortDescriptor(nil), node.Inputs...), node.Outputs...) {
			if port.Descriptor.Schema != descriptor.Schema().String() {
				t.Fatalf("bound descriptor schema = %s", port.Descriptor.Schema)
			}
		}
	}
}

func TestHostBindsChoicesToExplicitGraphOpenPorts(t *testing.T) {
	source, transform, sink, _ := boundaryComponents(nil)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, transform, sink))
	capabilities, _ := access.NewCapabilities(access.SequentialRead)
	provider, _ := access.DefineProvider[boundarySourceID]([]string{"memory"}, access.WithProviderCapabilities(capabilities))
	trait, _ := endpoint.NewTrait(endpoint.FiniteStatic, endpoint.Offline)
	sinkEndpoint, _ := endpoint.New(sink, trait)
	host, err := New(Plugins(set), Providers(provider), Endpoints(sinkEndpoint), PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}))
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := access.Parse("memory:data")
	input, _ := job.InputFromReference(reference)
	endpointRequest, _ := job.NewEndpoint(sink.Identity(), config.NewPatch())
	output, _ := job.OutputToEndpoint(endpointRequest)
	graph, err := job.NewGraph([]job.Node{job.NewNode("transform", transform.Identity(), config.NewPatch())}, nil)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := host.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(planned.Nodes()) != 3 || len(planned.Edges()) != 2 {
		t.Fatalf("explicit boundary Plan = nodes %d, edges %d", len(planned.Nodes()), len(planned.Edges()))
	}
}

func TestHostRejectsProviderCapabilityBeforeOpen(t *testing.T) {
	var opens atomic.Int32
	source, _, sink, _ := boundaryComponents(&opens)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, sink))
	capabilities, _ := access.NewCapabilities(access.RandomRead)
	provider, _ := access.DefineProvider[boundarySourceID](
		[]string{"memory"},
		access.WithProviderCapabilities(capabilities),
		access.WithProviderRequirements(access.NewRequirements(access.AnyOf(access.SequentialRead))),
	)
	trait, _ := endpoint.NewTrait(endpoint.FiniteStatic, endpoint.Offline)
	sinkEndpoint, _ := endpoint.New(sink, trait)
	host, err := New(Plugins(set), Providers(provider), Endpoints(sinkEndpoint))
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := access.Parse("memory:data")
	input, _ := job.InputFromReference(reference)
	endpointRequest, _ := job.NewEndpoint(sink.Identity(), config.NewPatch())
	output, _ := job.OutputToEndpoint(endpointRequest)
	request, _ := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
	_, err = host.Plan(context.Background(), request)
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "bind.capability" || opens.Load() != 0 {
		t.Fatalf("capability diagnostic = %#v, opens=%d", items, opens.Load())
	}
}

func TestHostBindsEndpointInBothDirections(t *testing.T) {
	source, _, sink, _ := boundaryComponents(nil)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, sink))
	trait, _ := endpoint.NewTrait(endpoint.LiveStatic, endpoint.Realtime)
	sourceEndpoint, _ := endpoint.New(source, trait)
	sinkEndpoint, _ := endpoint.New(sink, trait)
	host, err := New(Plugins(set), Endpoints(sourceEndpoint, sinkEndpoint))
	if err != nil {
		t.Fatal(err)
	}
	inputRequest, _ := job.NewEndpoint(source.Identity(), config.NewPatch())
	outputRequest, _ := job.NewEndpoint(sink.Identity(), config.NewPatch())
	input, _ := job.InputFromEndpoint(inputRequest)
	output, _ := job.OutputToEndpoint(outputRequest)
	request, _ := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
	planned, err := host.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range planned.Boundaries() {
		if binding.Kind != plan.EndpointBoundary || binding.Topology != endpoint.LiveStatic || binding.Mode != endpoint.Realtime {
			t.Fatalf("endpoint binding = %#v", binding)
		}
	}
}

func TestBoundaryPlanIsIndependentOfManifestOrder(t *testing.T) {
	source, _, sink, _ := boundaryComponents(nil)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, sink))
	capabilities, _ := access.NewCapabilities(access.SequentialRead)
	memory, _ := access.DefineProvider[boundarySourceID]([]string{"memory"}, access.WithProviderCapabilities(capabilities))
	unused, _ := access.DefineProvider[boundarySourceID]([]string{"unused"}, access.WithProviderCapabilities(capabilities))
	trait, _ := endpoint.NewTrait(endpoint.FiniteStatic, endpoint.Offline)
	sinkEndpoint, _ := endpoint.New(sink, trait)
	platform := PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"})
	left, err := New(Plugins(set), Providers(memory, unused), Endpoints(sinkEndpoint), platform)
	if err != nil {
		t.Fatal(err)
	}
	right, err := New(Plugins(set), Providers(unused, memory), Endpoints(sinkEndpoint), platform)
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := access.Parse("memory:data")
	input, _ := job.InputFromReference(reference)
	endpointRequest, _ := job.NewEndpoint(sink.Identity(), config.NewPatch())
	output, _ := job.OutputToEndpoint(endpointRequest)
	request, _ := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
	leftPlan, err := left.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	rightPlan, err := right.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if left.Catalog().Fingerprint() != right.Catalog().Fingerprint() || leftPlan.Fingerprint() != rightPlan.Fingerprint() {
		t.Fatal("Provider manifest order changed catalog or Plan identity")
	}
}

func boundaryComponents(opens *atomic.Int32) (plugin.Component, plugin.Component, plugin.Component, stream.Descriptor) {
	configuration := config.Struct[boundaryConfigID](func() boundaryConfig { return boundaryConfig{} }).Version("1").Build()
	descriptor := stream.MustDescriptor("boundary", boundarySchema.Identity(), timing.MustBase(1, 48_000), property.New())
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", boundarySchema)})
	transformShape := flow.NewShape([]flow.Port{flow.In("in", boundarySchema)}, []flow.Port{flow.Out("out", boundarySchema)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", boundarySchema)}, nil)
	component := func(shape flow.Shape, compile plugin.CompileFunc[boundaryConfig, boundaryPlan, stream.Descriptor]) plugin.ComponentOption {
		return plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Shape:   plugin.StaticShape[boundaryConfig](shape),
			Compile: compile,
			Open: func(plugin.OpenContext, boundaryPlan) (flow.Operator, error) {
				if opens != nil {
					opens.Add(1)
				}
				return boundaryOperator{shape: shape}, nil
			},
		})
	}
	source := plugin.NewComponent[boundarySourceID](plugin.Descriptor{DisplayName: "source"}, configuration, component(sourceShape, func(plugin.CompileContext, boundaryConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
		return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: sourceShape}, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor))}, nil
	}))
	transform := plugin.NewComponent[boundaryTransformID](plugin.Descriptor{DisplayName: "transform"}, configuration, component(transformShape, func(_ plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
		input, ok := inputs.One("in")
		if !ok {
			return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("boundary.input"))}}, nil
		}
		return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: transformShape}, Outputs: flow.NewDescriptors(flow.Describe("out", input))}, nil
	}))
	sink := plugin.NewComponent[boundarySinkID](plugin.Descriptor{DisplayName: "sink"}, configuration, component(sinkShape, func(_ plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
		if _, ok := inputs.One("in"); !ok {
			return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("boundary.input"))}}, nil
		}
		return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: sinkShape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
	}))
	return source, transform, sink, descriptor
}
