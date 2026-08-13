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
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type (
	boundaryPluginID      struct{}
	boundaryConfigID      struct{}
	boundaryFormatID      struct{}
	boundaryOtherFormatID struct{}
	boundarySourceID      struct{}
	boundaryTransformID   struct{}
	boundarySinkID        struct{}
)

type boundaryConfig struct{}
type boundaryPlan struct{ shape flow.Shape }

func boundaryFormat() mediaformat.Format {
	value, err := mediaformat.Define[boundaryFormatID](nil)
	if err != nil {
		panic(err)
	}
	return value
}

type boundaryOperator struct{ shape flow.Shape }
type boundarySourceOperator struct{ boundaryOperator }
type boundaryTransformOperator struct{ boundaryOperator }
type boundarySinkOperator struct{ boundaryOperator }
type boundaryByteSinkOperator struct{ boundaryOperator }
type boundarySession struct{ capabilities access.Capabilities }

func (o boundaryOperator) Ports() flow.Shape { return o.shape.Clone() }
func (boundaryOperator) Close() error        { return nil }
func (boundarySourceOperator) Read(context.Context, *flow.Item[buffer.Handle]) error {
	return io.EOF
}
func (boundaryTransformOperator) Process(ctx context.Context, input *flow.Item[buffer.Handle], output flow.Emitter[access.Write]) error {
	defer input.Drop()
	write, err := access.Append(input.Value().Share())
	if err != nil {
		return err
	}
	item := flow.NewItem(write, access.Writes())
	defer item.Drop()
	return output.Emit(ctx, &item)
}
func (boundaryTransformOperator) Flush(context.Context, flow.Emitter[access.Write]) error {
	return nil
}
func (boundarySinkOperator) Write(_ context.Context, input *flow.Item[access.Write]) error {
	input.Drop()
	return nil
}
func (boundaryByteSinkOperator) Write(_ context.Context, input *flow.Item[buffer.Handle]) error {
	input.Drop()
	return nil
}
func (boundarySinkOperator) Flush(context.Context) error         { return nil }
func (boundarySinkOperator) Sync(context.Context) error          { return nil }
func (boundarySinkOperator) PrepareCommit(context.Context) error { return nil }
func (boundarySinkOperator) Commit(context.Context) error        { return nil }
func (boundarySinkOperator) Abort(context.Context) error         { return nil }
func (boundaryByteSinkOperator) Flush(context.Context) error     { return nil }
func (boundaryByteSinkOperator) Sync(context.Context) error      { return nil }
func (boundaryByteSinkOperator) PrepareCommit(context.Context) error {
	return nil
}
func (boundaryByteSinkOperator) Commit(context.Context) error { return nil }
func (boundaryByteSinkOperator) Abort(context.Context) error  { return nil }
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
	source, transform, sink, _ := boundaryComponentsWith(
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
	graph, err := boundaryGraph(transform)
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
	if opens.Load() != 0 {
		t.Fatalf("binding or planning opened %d component(s)", opens.Load())
	}
	if len(planned.Nodes()) != 3 || len(planned.Edges()) != 2 || len(planned.Boundaries()) != 2 {
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
	readPorts, writePorts := 0, 0
	for _, node := range planned.Nodes() {
		for _, port := range append(append([]plan.PortDescriptor(nil), node.Inputs...), node.Outputs...) {
			switch port.Descriptor.Schema {
			case access.Bytes().Identity().String():
				readPorts++
			case access.Writes().Identity().String():
				writePorts++
			default:
				t.Fatalf("bound descriptor schema = %s", port.Descriptor.Schema)
			}
		}
	}
	if readPorts != 2 || writePorts != 2 {
		t.Fatalf("directional carrier ports = read %d, write %d", readPorts, writePorts)
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
	source, transform, sink, _ := boundaryComponentsWith(
		&opens,
		[]plugin.ComponentOption{access.Source("memory", capabilities, boundaryAcquire(capabilities))},
		nil,
	)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, transform, sink))
	host, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := access.Parse("memory:data")
	input, _ := job.InputFromReference(reference)
	output, _ := job.OutputToReference(reference)
	graph, _ := boundaryGraph(transform)
	request, _ := job.New([]job.Input{input}, []job.Output{output}, graph)
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
	source, transform, sink, _ := boundaryComponentsWith(
		nil,
		[]plugin.ComponentOption{endpoint.WithTrait(trait)},
		[]plugin.ComponentOption{endpoint.WithTrait(trait)},
	)
	set := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, transform, sink))
	host, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	inputRequest, _ := job.NewEndpoint(source.Identity(), config.NewPatch())
	outputRequest, _ := job.NewEndpoint(sink.Identity(), config.NewPatch())
	input, _ := job.InputFromEndpoint(inputRequest)
	output, _ := job.OutputToEndpoint(outputRequest)
	graph, _ := boundaryGraph(transform)
	request, _ := job.New([]job.Input{input}, []job.Output{output}, graph)
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
	source, transform, sink, _ := boundaryComponentsWith(
		nil,
		[]plugin.ComponentOption{access.Source("memory", sourceCapabilities, boundaryAcquire(sourceCapabilities))},
		[]plugin.ComponentOption{access.Sink("memory", sinkCapabilities, access.AtomicReplace, boundaryAcquire(sinkCapabilities))},
	)
	leftSet := plugin.NewSet().Add(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, transform, sink))
	rightSet := plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, sink, transform, source))
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
	graph, _ := boundaryGraph(transform)
	request, _ := job.New([]job.Input{input}, []job.Output{output}, graph)
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

func TestPinnedBoundaryFormatMustAgreeWithJobSelector(t *testing.T) {
	sourceCapabilities := mustCapabilities(t, access.SequentialRead)
	sinkCapabilities := mustCapabilities(t, access.SequentialWrite)
	source, transform, sink, _ := boundaryComponentsWith(
		nil,
		[]plugin.ComponentOption{access.Source("memory", sourceCapabilities, boundaryAcquire(sourceCapabilities))},
		[]plugin.ComponentOption{access.Sink("memory", sinkCapabilities, access.AtomicReplace, boundaryAcquire(sinkCapabilities))},
	)
	instance, err := New(Plugins(plugin.NewSet(plugin.Define[boundaryPluginID](plugin.Descriptor{DisplayName: "boundary fixture", Version: "1"}, source, transform, sink))))
	if err != nil {
		t.Fatal(err)
	}
	inputReference, _ := access.Parse("memory:input")
	outputReference, _ := access.Parse("memory:output")
	baseInput, _ := job.InputFromReference(inputReference)
	baseOutput, _ := job.OutputToReference(outputReference)
	graph, _ := boundaryGraph(transform)
	matching, _ := job.SelectFormat(boundaryFormat())
	otherFormat, _ := mediaformat.Define[boundaryOtherFormatID](nil)
	conflicting, _ := job.SelectFormat(otherFormat)

	matchingInput, _ := baseInput.WithFormatHint(matching)
	matchingOutput, _ := baseOutput.WithFormatRequest(matching)
	request, _ := job.New([]job.Input{matchingInput}, []job.Output{matchingOutput}, graph)
	if _, err := instance.Plan(t.Context(), request); err != nil {
		t.Fatalf("matching pinned selectors failed: %v", err)
	}
	for name, choices := range map[string]struct {
		input  job.Input
		output job.Output
	}{
		"input":  {input: mustInputHint(t, baseInput, conflicting), output: baseOutput},
		"output": {input: baseInput, output: mustOutputRequest(t, baseOutput, conflicting)},
	} {
		t.Run(name, func(t *testing.T) {
			request, _ := job.New([]job.Input{choices.input}, []job.Output{choices.output}, graph)
			_, err := instance.Plan(t.Context(), request)
			items := diagnostic.ItemsOf(err)
			if len(items) != 1 || items[0].Code != "bind.format-conflict" || items[0].Detail["pinnedFormat"] != boundaryFormat().Identity().String() {
				t.Fatalf("pinned conflict = %#v, %v", items, err)
			}
		})
	}
}

func mustInputHint(t testing.TB, input job.Input, selector job.FormatSelector) job.Input {
	t.Helper()
	result, err := input.WithFormatHint(selector)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustOutputRequest(t testing.TB, output job.Output, selector job.FormatSelector) job.Output {
	t.Helper()
	result, err := output.WithFormatRequest(selector)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHostReportsMissingProbeCandidatesAndUnsatisfiedFormatRequirements(t *testing.T) {
	trait, _ := endpoint.NewTrait(endpoint.FiniteStatic, endpoint.Offline)
	tests := map[string]struct {
		capabilities access.Capabilities
		sink         func() plugin.Component
		code         string
	}{
		"missing": {
			capabilities: mustCapabilities(t, access.SequentialRead),
			sink:         func() plugin.Component { return boundarySinkWithoutFormat(trait) },
			code:         "prepare.probe-candidate",
		},
		"unsatisfied": {
			capabilities: mustCapabilities(t, access.StableSize),
			sink:         func() plugin.Component { return boundaryReadSink(trait, true) },
			code:         "bind.capability-unsatisfied",
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

func boundaryComponentsWith(opens *atomic.Int32, sourceTraits, sinkTraits []plugin.ComponentOption, readOptions ...mediaformat.ReadOption) (plugin.Component, plugin.Component, plugin.Component, stream.Descriptor) {
	return boundaryComponentsWithRequirements(
		opens,
		sourceTraits,
		sinkTraits,
		access.NewRequirements(access.AllOf(access.SequentialRead), access.AllOf(access.RandomRead)),
		readOptions...,
	)
}

func boundaryComponentsWithRequirements(opens *atomic.Int32, sourceTraits, sinkTraits []plugin.ComponentOption, requirements access.Requirements, readOptions ...mediaformat.ReadOption) (plugin.Component, plugin.Component, plugin.Component, stream.Descriptor) {
	configuration := config.Struct[boundaryConfigID](func() boundaryConfig { return boundaryConfig{} }).Version("1").Build()
	descriptor := stream.MustDescriptor("boundary", access.Bytes().Identity(), access.CarrierTimeBase(), property.New())
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", access.Bytes())})
	transformShape := flow.NewShape([]flow.Port{flow.In("in", access.Bytes())}, []flow.Port{flow.Out("out", access.Writes())})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", access.Writes())}, nil)
	component := func(shape flow.Shape, compile plugin.CompileFunc[boundaryConfig, boundaryPlan, stream.Descriptor], operator func(flow.Shape) flow.Operator) plugin.ComponentOption {
		return plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Shape:   plugin.StaticShape[boundaryConfig](shape),
			Compile: compile,
			Open: func(plugin.OpenContext, boundaryPlan) (flow.Operator, error) {
				if opens != nil {
					opens.Add(1)
				}
				return operator(shape.Clone()), nil
			},
		})
	}
	sourceOptions := append([]plugin.ComponentOption{component(sourceShape, func(plugin.CompileContext, boundaryConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
		return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: sourceShape}, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor))}, nil
	}, func(shape flow.Shape) flow.Operator {
		return boundarySourceOperator{boundaryOperator{shape: shape}}
	}), plugin.WithReader("out", access.Bytes())}, sourceTraits...)
	source := plugin.NewComponent[boundarySourceID](plugin.Descriptor{DisplayName: "source"}, configuration, sourceOptions...)
	transform := plugin.NewComponent[boundaryTransformID](plugin.Descriptor{DisplayName: "transform"}, configuration, component(transformShape, func(_ plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
		input, ok := inputs.One("in")
		if !ok {
			return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("boundary.input"))}}, nil
		}
		output, err := stream.NewDescriptor(input.ID(), access.Writes().Identity(), access.CarrierTimeBase(), property.New())
		if err != nil {
			return plugin.Compiled[boundaryPlan, stream.Descriptor]{}, err
		}
		return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: transformShape}, Outputs: flow.NewDescriptors(flow.Describe("out", output.WithMetadata(input.Metadata())))}, nil
	}, func(shape flow.Shape) flow.Operator {
		return boundaryTransformOperator{boundaryOperator{shape: shape}}
	}), plugin.WithProcessor("in", access.Bytes(), "out", access.Writes()),
		mediaformat.Read(boundaryFormat(), requirements, readOptions...),
		mediaformat.Write(boundaryFormat(), access.NewRequirements(access.AllOf(access.SequentialWrite))),
	)
	sinkOptions := append([]plugin.ComponentOption{component(sinkShape, func(_ plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
		if _, ok := inputs.One("in"); !ok {
			return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("boundary.input"))}}, nil
		}
		return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: sinkShape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
	}, func(shape flow.Shape) flow.Operator {
		return boundarySinkOperator{boundaryOperator{shape: shape}}
	}), plugin.WithWriter("in", access.Writes())}, sinkTraits...)
	sink := plugin.NewComponent[boundarySinkID](plugin.Descriptor{DisplayName: "sink"}, configuration, sinkOptions...)
	return source, transform, sink, descriptor
}

func boundarySinkWithoutFormat(trait endpoint.Trait) plugin.Component {
	return boundaryReadSink(trait, false)
}

func boundaryReadSink(trait endpoint.Trait, withFormat bool) plugin.Component {
	configuration := config.Struct[boundaryConfigID](func() boundaryConfig { return boundaryConfig{} }).Version("1").Build()
	shape := flow.NewShape([]flow.Port{flow.In("in", access.Bytes())}, nil)
	options := []plugin.ComponentOption{
		plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Shape: plugin.StaticShape[boundaryConfig](shape),
			Compile: func(plugin.CompileContext, boundaryConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
				return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: shape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
			},
			Open: func(plugin.OpenContext, boundaryPlan) (flow.Operator, error) {
				return boundaryByteSinkOperator{boundaryOperator{shape: shape}}, nil
			},
		}),
		plugin.WithWriter("in", access.Bytes()),
		endpoint.WithTrait(trait),
	}
	if withFormat {
		options = append(options, mediaformat.Read(boundaryFormat(), access.NewRequirements(access.AllOf(access.SequentialRead), access.AllOf(access.RandomRead))))
	}
	return plugin.NewComponent[boundarySinkID](
		plugin.Descriptor{DisplayName: "sink"},
		configuration,
		options...,
	)
}

func boundaryGraph(transform plugin.Component) (job.Graph, error) {
	return job.NewGraph([]job.Node{job.NewNode("transform", transform.Identity(), config.NewPatch())}, nil)
}
