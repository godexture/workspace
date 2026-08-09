package host

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/buffer"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type (
	boundaryPluginID    struct{}
	boundaryConfigID    struct{}
	boundaryFormatID    struct{}
	boundarySourceID    struct{}
	boundaryTransformID struct{}
	boundarySinkID      struct{}
)

type boundaryConfig struct{}
type boundaryPlan struct{ shape flow.Shape }

var boundarySchema = access.Bytes()

func boundaryFormat() mediaformat.Format {
	value, err := mediaformat.Define[boundaryFormatID](nil)
	if err != nil {
		panic(err)
	}
	return value
}

type boundaryOperator struct{ shape flow.Shape }
type boundarySession struct{ capabilities access.Capabilities }

func (o boundaryOperator) Ports() flow.Shape { return o.shape.Clone() }
func (boundaryOperator) Close() error        { return nil }
func (boundaryOperator) Read(context.Context) (flow.Input[buffer.Handle], error) {
	return flow.Input[buffer.Handle]{}, io.EOF
}
func (boundaryOperator) Write(_ context.Context, input flow.Input[buffer.Handle]) error {
	input.Drop()
	return nil
}
func (boundaryOperator) Flush(context.Context) error         { return nil }
func (boundaryOperator) Sync(context.Context) error          { return nil }
func (boundaryOperator) PrepareCommit(context.Context) error { return nil }
func (boundaryOperator) Commit(context.Context) error        { return nil }
func (boundaryOperator) Abort(context.Context) error         { return nil }
func (s boundarySession) Capabilities() access.Capabilities {
	result, _ := access.NewCapabilities(s.capabilities.Values()...)
	return result
}
func (boundarySession) Close() error { return nil }
func (boundarySession) Read(context.Context, []byte) (int, error) {
	return 0, io.EOF
}

func boundaryAcquire(capabilities access.Capabilities) access.AcquireFunc {
	return func(context.Context, access.Reference, access.Selection) (access.Session, error) {
		return boundarySession{capabilities: capabilities}, nil
	}
}

func TestHostBindsProviderAndEndpointWithoutOpening(t *testing.T) {
	var opens atomic.Int32
	capabilities, err := access.NewCapabilities(access.RandomRead, access.SequentialRead, access.StableSize)
	if err != nil {
		t.Fatal(err)
	}
	trait, err := endpoint.NewTrait(endpoint.FiniteStatic, endpoint.Offline)
	if err != nil {
		t.Fatal(err)
	}
	source, transform, sink, descriptor := boundaryComponentsWith(
		&opens,
		[]plugin.ComponentOption{access.Source("memory", capabilities, boundaryAcquire(capabilities))},
		[]plugin.ComponentOption{endpoint.WithTrait(trait)},
	)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, transform, sink))
	host, err := New(
		Plugins(set),
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
	capabilities, _ := access.NewCapabilities(access.SequentialRead)
	trait, _ := endpoint.NewTrait(endpoint.FiniteStatic, endpoint.Offline)
	source, transform, sink, _ := boundaryComponentsWith(
		nil,
		[]plugin.ComponentOption{access.Source("memory", capabilities, boundaryAcquire(capabilities))},
		[]plugin.ComponentOption{endpoint.WithTrait(trait)},
	)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, transform, sink))
	host, err := New(Plugins(set), PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}))
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

func TestHostRejectsReferenceWithoutTraitForRequestedDirection(t *testing.T) {
	var opens atomic.Int32
	capabilities, _ := access.NewCapabilities(access.RandomRead)
	source, _, sink, _ := boundaryComponentsWith(
		&opens,
		[]plugin.ComponentOption{access.Source("memory", capabilities, boundaryAcquire(capabilities))},
		nil,
	)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, sink))
	host, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := access.Parse("memory:data")
	input, _ := job.InputFromReference(reference)
	output, _ := job.OutputToReference(reference)
	request, _ := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
	_, err = host.Plan(context.Background(), request)
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "bind.provider-not-found" || items[0].Detail["direction"] != "2" || opens.Load() != 0 {
		t.Fatalf("directional provider diagnostic = %#v, opens=%d", items, opens.Load())
	}
}

func TestHostRejectsInvalidTraitComposition(t *testing.T) {
	capabilities, _ := access.NewCapabilities(access.SequentialRead)
	acquire := boundaryAcquire(capabilities)
	tests := map[string]struct {
		sourceTraits []plugin.ComponentOption
		sinkTraits   []plugin.ComponentOption
		code         string
	}{
		"duplicate key": {
			sourceTraits: []plugin.ComponentOption{
				access.Source("memory", capabilities, acquire),
				access.Source("other", capabilities, acquire),
			},
			code: "plugin.trait-duplicate",
		},
		"directional shape": {
			sinkTraits: []plugin.ComponentOption{access.Source("memory", capabilities, acquire)},
			code:       "catalog.access-shape",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			source, _, sink, _ := boundaryComponentsWith(nil, test.sourceTraits, test.sinkTraits)
			set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, sink))
			_, err := New(Plugins(set))
			if err == nil {
				t.Fatalf("Host accepted invalid trait composition")
			}
			for _, item := range diagnostic.ItemsOf(err) {
				if item.Code == test.code {
					return
				}
			}
			t.Fatalf("diagnostic %s = %v", test.code, err)
		})
	}
}

func TestHostBindsEndpointInBothDirections(t *testing.T) {
	trait, _ := endpoint.NewTrait(endpoint.LiveStatic, endpoint.Realtime)
	source, _, sink, _ := boundaryComponentsWith(
		nil,
		[]plugin.ComponentOption{endpoint.WithTrait(trait)},
		[]plugin.ComponentOption{endpoint.WithTrait(trait)},
	)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, sink))
	host, err := New(Plugins(set))
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

func TestHostBindsSourceAndSinkTraitsForSameSchemeFromPluginSet(t *testing.T) {
	sourceCapabilities, _ := access.NewCapabilities(access.SequentialRead)
	sinkCapabilities, _ := access.NewCapabilities(access.SequentialWrite)
	source, _, sink, _ := boundaryComponentsWith(
		nil,
		[]plugin.ComponentOption{access.Source("memory", sourceCapabilities, boundaryAcquire(sourceCapabilities))},
		[]plugin.ComponentOption{access.Sink("memory", sinkCapabilities, access.AtomicReplace, boundaryAcquire(sinkCapabilities))},
	)
	leftSet := plugin.NewSet().Add(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, sink))
	rightSet := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, sink, source))
	platform := PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"})
	left, err := New(Plugins(leftSet), platform)
	if err != nil {
		t.Fatal(err)
	}
	right, err := New(Plugins(rightSet), platform)
	if err != nil {
		t.Fatal(err)
	}
	inputReference, _ := access.Parse("memory:input")
	outputReference, _ := access.Parse("memory:output")
	input, _ := job.InputFromReference(inputReference)
	output, _ := job.OutputToReference(outputReference)
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
		t.Fatal("trait component order changed catalog or Plan identity")
	}
	boundaries := leftPlan.Boundaries()
	if len(boundaries) != 2 || boundaries[0].Component != source.Identity().String() || boundaries[1].Component != sink.Identity().String() {
		t.Fatalf("directional traits = %#v", boundaries)
	}
	if len(boundaries[0].Selected) != 1 || boundaries[0].Selected[0] != access.SequentialRead || len(boundaries[1].Selected) != 1 || boundaries[1].Selected[0] != access.SequentialWrite {
		t.Fatalf("directional capability selection = %#v", boundaries)
	}
}

func TestHostReportsMissingAndUnsatisfiedFormatRequirements(t *testing.T) {
	trait, _ := endpoint.NewTrait(endpoint.FiniteStatic, endpoint.Offline)
	tests := map[string]struct {
		capabilities access.Capabilities
		sink         func() plugin.Component
		code         string
	}{
		"missing": {
			capabilities: mustCapabilities(t, access.SequentialRead),
			sink:         func() plugin.Component { return boundarySinkWithoutFormat(trait) },
			code:         "bind.format-requirement",
		},
		"unsatisfied": {
			capabilities: mustCapabilities(t, access.StableSize),
			sink: func() plugin.Component {
				_, _, sink, _ := boundaryComponentsWith(nil, nil, []plugin.ComponentOption{endpoint.WithTrait(trait)})
				return sink
			},
			code: "bind.capability-unsatisfied",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			source, _, _, _ := boundaryComponentsWith(nil, []plugin.ComponentOption{access.Source("memory", test.capabilities, boundaryAcquire(test.capabilities))}, nil)
			sink := test.sink()
			set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, sink))
			instance, err := New(Plugins(set))
			if err != nil {
				t.Fatal(err)
			}
			reference, _ := access.Parse("memory:data")
			input, _ := job.InputFromReference(reference)
			requestValue, _ := job.NewEndpoint(sink.Identity(), config.NewPatch())
			output, _ := job.OutputToEndpoint(requestValue)
			request, _ := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
			_, err = instance.Plan(context.Background(), request)
			items := diagnostic.ItemsOf(err)
			if len(items) != 1 || items[0].Code != test.code || items[0].Detail["scheme"] != "memory" || items[0].Detail["direction"] != "read" {
				t.Fatalf("diagnostic %s = %#v", test.code, items)
			}
			if name == "unsatisfied" && (items[0].Detail["available"] != "stable-size" || items[0].Detail["alternatives"] == "") {
				t.Fatalf("capability detail = %#v", items[0].Detail)
			}
		})
	}
}

func mustCapabilities(t *testing.T, values ...access.Capability) access.Capabilities {
	t.Helper()
	result, err := access.NewCapabilities(values...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func boundaryComponents(opens *atomic.Int32) (plugin.Component, plugin.Component, plugin.Component, stream.Descriptor) {
	return boundaryComponentsWith(opens, nil, nil)
}

func boundaryComponentsWith(opens *atomic.Int32, sourceTraits, sinkTraits []plugin.ComponentOption) (plugin.Component, plugin.Component, plugin.Component, stream.Descriptor) {
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
	sourceOptions := append([]plugin.ComponentOption{component(sourceShape, func(plugin.CompileContext, boundaryConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
		return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: sourceShape}, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor))}, nil
	}), plugin.WithReader("out", boundarySchema), mediaformat.Write(boundaryFormat(), access.AnyOf(access.SequentialWrite))}, sourceTraits...)
	source := plugin.NewComponent[boundarySourceID](plugin.Descriptor{DisplayName: "source"}, configuration, sourceOptions...)
	transform := plugin.NewComponent[boundaryTransformID](plugin.Descriptor{DisplayName: "transform"}, configuration, component(transformShape, func(_ plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
		input, ok := inputs.One("in")
		if !ok {
			return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("boundary.input"))}}, nil
		}
		return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: transformShape}, Outputs: flow.NewDescriptors(flow.Describe("out", input))}, nil
	}), plugin.WithProcessor("in", boundarySchema, "out", boundarySchema),
		mediaformat.Read(boundaryFormat(), access.AnyOf(access.SequentialRead), access.AnyOf(access.RandomRead)),
		mediaformat.Write(boundaryFormat(), access.AnyOf(access.SequentialWrite)),
	)
	sinkOptions := append([]plugin.ComponentOption{component(sinkShape, func(_ plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
		if _, ok := inputs.One("in"); !ok {
			return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("boundary.input"))}}, nil
		}
		return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: sinkShape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
	}), plugin.WithWriter("in", boundarySchema), mediaformat.Read(boundaryFormat(), access.AnyOf(access.SequentialRead), access.AnyOf(access.RandomRead))}, sinkTraits...)
	sink := plugin.NewComponent[boundarySinkID](plugin.Descriptor{DisplayName: "sink"}, configuration, sinkOptions...)
	return source, transform, sink, descriptor
}

func boundarySinkWithoutFormat(trait endpoint.Trait) plugin.Component {
	configuration := config.Struct[boundaryConfigID](func() boundaryConfig { return boundaryConfig{} }).Version("1").Build()
	shape := flow.NewShape([]flow.Port{flow.In("in", access.Bytes())}, nil)
	return plugin.NewComponent[boundarySinkID](
		plugin.Descriptor{DisplayName: "sink"},
		configuration,
		plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Shape: plugin.StaticShape[boundaryConfig](shape),
			Compile: func(plugin.CompileContext, boundaryConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
				return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: shape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
			},
			Open: func(plugin.OpenContext, boundaryPlan) (flow.Operator, error) {
				return boundaryOperator{shape: shape}, nil
			},
		}),
		plugin.WithWriter("in", access.Bytes()),
		endpoint.WithTrait(trait),
	)
}
