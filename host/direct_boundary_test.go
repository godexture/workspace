package host

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type directBoundaryPluginID struct{}
type directBoundaryFormatID struct{}
type directBoundaryReaderID struct{}
type directBoundaryOneReaderID struct{}
type directBoundaryMuxID struct{}

type directBoundaryReader struct{ shape flow.Shape }
type directBoundaryOneReader struct{ shape flow.Shape }
type directBoundaryMux struct{ shape flow.Shape }

func (o directBoundaryReader) Ports() flow.Shape { return o.shape.Clone() }
func (directBoundaryReader) Close() error        { return nil }
func (directBoundaryReader) Read(context.Context, flow.RoutedEmitter[access.Write]) error {
	return io.EOF
}
func (o directBoundaryOneReader) Ports() flow.Shape { return o.shape.Clone() }
func (directBoundaryOneReader) Close() error        { return nil }
func (directBoundaryOneReader) Read(context.Context, *flow.Item[access.Write]) error {
	return io.EOF
}
func (o directBoundaryMux) Ports() flow.Shape { return o.shape.Clone() }
func (directBoundaryMux) Close() error        { return nil }
func (directBoundaryMux) Process(_ context.Context, item *flow.Item[access.Write], _ flow.Emitter[access.Write]) error {
	item.Drop()
	return nil
}
func (directBoundaryMux) Flush(context.Context, flow.Emitter[access.Write]) error { return nil }

func directBoundaryFormat() mediaformat.Format {
	value, err := mediaformat.Define[directBoundaryFormatID](nil)
	if err != nil {
		panic(err)
	}
	return value
}

func directBoundaryReaderComponent(opens *atomic.Int32) plugin.Component {
	return directBoundaryReaderComponentWithInspect(opens, false)
}

func directBoundaryOneReaderComponent(opens *atomic.Int32) plugin.Component {
	shape := flow.NewShape(nil, []flow.Port{flow.Out("packets", access.Writes())})
	configuration := config.Struct[boundaryConfigID](func() boundaryConfig { return boundaryConfig{} }).Version("1").Build()
	return plugin.NewComponent[directBoundaryOneReaderID](plugin.Descriptor{DisplayName: "direct one reader"}, configuration,
		plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Ports: shape,
			Compile: func(plugin.CompileContext, boundaryConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
				descriptor := stream.MustDescriptor("direct", access.Writes().Descriptor(), timing.Base{}, property.New())
				return plugin.Compiled[boundaryPlan, stream.Descriptor]{
					Plan:    boundaryPlan{shape: shape},
					Outputs: flow.NewDescriptors(flow.Describe("packets", descriptor)),
					Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "direct.reader"}},
				}, nil
			},
			Open: func(ctx plugin.OpenContext, value boundaryPlan) (flow.Operator, error) {
				if _, ok := plugin.Boundary[access.Opening](ctx); ok {
					return nil, errors.New("direct one reader received a duplicate boundary opening")
				}
				if _, ok := mediaformat.SourceOpening(ctx); !ok {
					return nil, errors.New("direct one reader did not receive the source opening")
				}
				opens.Add(1)
				return directBoundaryOneReader{shape: value.shape.Clone()}, nil
			},
		}),
		plugin.WithReader("packets", access.Writes()),
		mediaformat.Read(directBoundaryFormat(), access.NewRequirements(access.AllOf(access.SequentialRead)), mediaformat.WithProbe(func(mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
			return mediaformat.Fallback(), nil
		})),
	)
}

func directBoundaryReaderComponentWithInspect(opens *atomic.Int32, inspect bool) plugin.Component {
	shape := flow.NewShape(nil, []flow.Port{flow.Out("packets", access.Writes(), flow.Many())})
	configuration := config.Struct[boundaryConfigID](func() boundaryConfig { return boundaryConfig{} }).Version("1").Build()
	options := []mediaformat.ReadOption{mediaformat.WithProbe(func(mediaformat.ProbeContext) (mediaformat.ProbeResult, error) {
		return mediaformat.Fallback(), nil
	})}
	if inspect {
		options = append(options, mediaformat.WithInspect(func(mediaformat.InspectContext) (mediaformat.Inspection, error) {
			return mediaformat.NewInspection(directBoundaryFormat(), 1), nil
		}))
	}
	return plugin.NewComponent[directBoundaryReaderID](plugin.Descriptor{DisplayName: "direct reader"}, configuration,
		plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Ports: shape,
			Compile: func(plugin.CompileContext, boundaryConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
				descriptor := stream.MustDescriptor("direct", access.Writes().Descriptor(), timing.Base{}, property.New())
				return plugin.Compiled[boundaryPlan, stream.Descriptor]{
					Plan:    boundaryPlan{shape: shape},
					Outputs: flow.NewDescriptors(flow.Describe("packets", descriptor)),
					Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "direct.reader"}},
				}, nil
			},
			Open: func(ctx plugin.OpenContext, value boundaryPlan) (flow.Operator, error) {
				if _, ok := plugin.Boundary[access.Opening](ctx); ok {
					return nil, errors.New("direct reader received a duplicate boundary opening")
				}
				if _, ok := mediaformat.SourceOpening(ctx); !ok {
					return nil, errors.New("direct reader did not receive the source opening")
				}
				opens.Add(1)
				return directBoundaryReader{shape: value.shape.Clone()}, nil
			},
		}),
		plugin.WithRoutedReader("packets", access.Writes()),
		mediaformat.Read(directBoundaryFormat(), access.NewRequirements(access.AllOf(access.SequentialRead)), options...),
	)
}

func directBoundaryMuxComponent(opens *atomic.Int32) plugin.Component {
	shape := flow.NewShape([]flow.Port{flow.In("packets", access.Writes())}, []flow.Port{flow.Out("writes", access.Writes())})
	configuration := config.Struct[boundaryConfigID](func() boundaryConfig { return boundaryConfig{} }).Version("1").Build()
	return plugin.NewComponent[directBoundaryMuxID](plugin.Descriptor{DisplayName: "direct mux"}, configuration,
		plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Ports: shape,
			Compile: func(ctx plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
				if _, ok := mediaformat.InspectionOf[int](ctx, directBoundaryFormat()); !ok {
					return plugin.Compiled[boundaryPlan, stream.Descriptor]{}, errors.New("direct mux did not receive the source inspection")
				}
				input, ok := inputs.One("packets")
				if !ok {
					return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("packets", plugin.ConditionNeed[stream.Descriptor]("direct.mux.input"))}}, nil
				}
				return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: shape}, Outputs: flow.NewDescriptors(flow.Describe("writes", input))}, nil
			},
			Open: func(ctx plugin.OpenContext, value boundaryPlan) (flow.Operator, error) {
				if _, ok := plugin.Boundary[access.Opening](ctx); ok {
					return nil, errors.New("direct mux received a boundary opening")
				}
				if _, ok := mediaformat.SourceOpening(ctx); !ok {
					return nil, errors.New("direct mux did not receive the source opening")
				}
				opens.Add(1)
				return directBoundaryMux{shape: value.shape.Clone()}, nil
			},
		}),
		plugin.WithProcessor("packets", access.Writes(), "writes", access.Writes()),
		mediaformat.Write(directBoundaryFormat(), access.NewRequirements(access.AllOf(access.SequentialWrite))),
	)
}

func TestReferenceInputDirectReaderUsesAnchorWithoutCarrier(t *testing.T) {
	readers := []struct {
		name  string
		build func(*atomic.Int32) plugin.Component
	}{
		{name: "one", build: directBoundaryOneReaderComponent},
		{name: "many", build: directBoundaryReaderComponent},
	}
	for _, test := range readers {
		t.Run(test.name, func(t *testing.T) {
			capabilities := mustCapabilities(t, access.SequentialRead)
			var acquired atomic.Int32
			acquire := func(context.Context, access.Reference, access.Selection) (access.Session, error) {
				acquired.Add(1)
				return boundarySession{capabilities: capabilities}, nil
			}
			source, _, sink, _ := boundaryComponentsWith(nil, []plugin.ComponentOption{access.Source("direct", capabilities, acquire)}, nil)
			var opens atomic.Int32
			reader := test.build(&opens)
			instance, err := New(Plugins(plugin.NewSet(plugin.Define[directBoundaryPluginID](plugin.Descriptor{DisplayName: "direct boundary", Version: "1"}, source, reader, sink))))
			if err != nil {
				t.Fatal(err)
			}

			graph, err := job.NewGraph(
				[]job.Node{
					job.NewNode("demux", reader.Identity(), config.NewPatch()),
					job.NewNode("sink", sink.Identity(), config.NewPatch()),
				},
				[]job.Edge{job.Connect(job.At("demux", "packets"), job.At("sink", "in"))},
			)
			if err != nil {
				t.Fatal(err)
			}
			reference, _ := access.Parse("direct:input")
			input, _ := job.InputFromReference(reference)
			request, err := job.New([]job.Input{input}, nil, graph)
			if err != nil {
				t.Fatal(err)
			}
			normalized, err := bind.Normalize(instance.bindings, request)
			if err != nil {
				t.Fatal(err)
			}
			entries := normalized.Boundaries().Entries()
			if len(entries) != 1 || entries[0].Anchor() != job.NodeID("demux") {
				t.Fatalf("capability selection lost direct anchor: %#v", entries)
			}

			planned, err := instance.Plan(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(planned.Nodes()); got != 2 {
				t.Fatalf("direct Plan nodes = %d, want 2 without a provider carrier", got)
			}
			if got := len(planned.Edges()); got != 1 {
				t.Fatalf("direct Plan edges = %d, want 1 without a provider edge", got)
			}
			boundaries := planned.Boundaries()
			if len(boundaries) != 1 || boundaries[0].Node != "demux" || boundaries[0].Port != "packets" || boundaries[0].Component != source.Identity().String() || len(boundaries[0].Selected) != 1 || boundaries[0].Selected[0] != access.SequentialRead {
				t.Fatalf("direct boundary projection = %#v", boundaries)
			}
			if acquired.Load() != 1 || opens.Load() != 0 {
				t.Fatalf("Plan lifecycle = acquisitions %d, opens %d", acquired.Load(), opens.Load())
			}
			if _, err := instance.Run(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if acquired.Load() != 2 || opens.Load() != 1 {
				t.Fatalf("Run lifecycle = acquisitions %d, opens %d", acquired.Load(), opens.Load())
			}
		})
	}
}

func TestAutomaticDirectReaderUsesAnchorWithoutCarrier(t *testing.T) {
	readers := []struct {
		name  string
		build func(*atomic.Int32) plugin.Component
	}{
		{name: "one", build: directBoundaryOneReaderComponent},
		{name: "many", build: directBoundaryReaderComponent},
	}
	for _, test := range readers {
		t.Run(test.name, func(t *testing.T) {
			capabilities := mustCapabilities(t, access.SequentialRead)
			var acquired atomic.Int32
			acquire := func(context.Context, access.Reference, access.Selection) (access.Session, error) {
				acquired.Add(1)
				return boundarySession{capabilities: capabilities}, nil
			}
			source, _, sink, _ := boundaryComponentsWith(nil, []plugin.ComponentOption{access.Source("direct-auto", capabilities, acquire)}, nil)
			var opens atomic.Int32
			reader := test.build(&opens)
			instance, err := New(Plugins(plugin.NewSet(plugin.Define[directBoundaryPluginID](plugin.Descriptor{DisplayName: "automatic direct boundary", Version: "1"}, source, reader, sink))))
			if err != nil {
				t.Fatal(err)
			}
			graph, err := job.NewGraph([]job.Node{job.NewNode("sink", sink.Identity(), config.NewPatch())}, nil)
			if err != nil {
				t.Fatal(err)
			}
			reference, _ := access.Parse("direct-auto:input")
			input, _ := job.InputFromReference(reference)
			input, err = input.WithFormatHint(mustFormatSelector(t, directBoundaryFormat()))
			if err != nil {
				t.Fatal(err)
			}
			request, err := job.New([]job.Input{input}, nil, graph)
			if err != nil {
				t.Fatal(err)
			}
			planned, err := instance.Plan(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if len(planned.Nodes()) != 2 || len(planned.Edges()) != 1 {
				t.Fatalf("automatic direct Plan = nodes %#v edges %#v", planned.Nodes(), planned.Edges())
			}
			for _, node := range planned.Nodes() {
				if node.Component == source.Identity().String() {
					t.Fatalf("automatic direct Plan retained provider carrier: %#v", planned.Nodes())
				}
			}
			boundaries := planned.Boundaries()
			if len(boundaries) != 1 || boundaries[0].Node == "input-0" || boundaries[0].Port != "packets" || boundaries[0].Component != source.Identity().String() || len(boundaries[0].Selected) != 1 || boundaries[0].Selected[0] != access.SequentialRead {
				t.Fatalf("automatic direct boundary = %#v", boundaries)
			}
			if acquired.Load() != 1 || opens.Load() != 0 {
				t.Fatalf("automatic Plan lifecycle = acquisitions %d, opens %d", acquired.Load(), opens.Load())
			}
			if _, err := instance.Run(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if acquired.Load() != 2 || opens.Load() != 1 {
				t.Fatalf("automatic Run lifecycle = acquisitions %d, opens %d", acquired.Load(), opens.Load())
			}
		})
	}
}

func TestDirectReaderHandsOneSourceSessionToInspectedMux(t *testing.T) {
	capabilities := mustCapabilities(t, access.SequentialRead)
	var acquired atomic.Int32
	acquire := func(context.Context, access.Reference, access.Selection) (access.Session, error) {
		acquired.Add(1)
		return boundarySession{capabilities: capabilities}, nil
	}
	source, _, sink, _ := boundaryComponentsWith(nil, []plugin.ComponentOption{access.Source("direct-handoff", capabilities, acquire)}, nil)
	var readerOpens atomic.Int32
	var muxOpens atomic.Int32
	reader := directBoundaryReaderComponentWithInspect(&readerOpens, true)
	mux := directBoundaryMuxComponent(&muxOpens)
	instance, err := New(Plugins(plugin.NewSet(plugin.Define[directBoundaryPluginID](plugin.Descriptor{DisplayName: "direct handoff", Version: "1"}, source, reader, mux, sink))))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("demux", reader.Identity(), config.NewPatch()),
			job.NewNode("mux", mux.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("demux", "packets"), job.At("mux", "packets")),
			job.Connect(job.At("mux", "writes"), job.At("sink", "in")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := access.Parse("direct-handoff:input")
	input, _ := job.InputFromReference(reference)
	request, err := job.New([]job.Input{input}, nil, graph)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Plan(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if acquired.Load() != 1 || readerOpens.Load() != 0 || muxOpens.Load() != 0 {
		t.Fatalf("Plan handoff lifecycle = acquisitions %d, reader opens %d, mux opens %d", acquired.Load(), readerOpens.Load(), muxOpens.Load())
	}
	if _, err := instance.Run(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if acquired.Load() != 2 || readerOpens.Load() != 1 || muxOpens.Load() != 1 {
		t.Fatalf("Run handoff lifecycle = acquisitions %d, reader opens %d, mux opens %d", acquired.Load(), readerOpens.Load(), muxOpens.Load())
	}
}

func TestDirectReaderRejectsNonReferenceInputsAndMissingInput(t *testing.T) {
	capabilities := mustCapabilities(t, access.SequentialRead)
	source, transform, sink, _ := boundaryComponentsWith(nil, []plugin.ComponentOption{access.Source("direct-kind", capabilities, boundaryAcquire(capabilities))}, nil)
	reader := directBoundaryReaderComponent(&atomic.Int32{})
	instance, err := New(Plugins(plugin.NewSet(plugin.Define[directBoundaryPluginID](plugin.Descriptor{DisplayName: "direct kinds", Version: "1"}, source, transform, reader, sink))))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("demux", reader.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("demux", "packets"), job.At("sink", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := job.NewEndpoint(sink.Identity(), config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	adaptor, err := job.NewAdaptor(sink.Identity(), config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	endpointInput, _ := job.InputFromEndpoint(endpoint)
	directInput, _ := job.InputFromSource(access.Borrow(struct{}{}), adaptor)
	for name, input := range map[string]job.Input{
		"endpoint": endpointInput,
		"direct":   directInput,
	} {
		t.Run(name, func(t *testing.T) {
			request, err := job.New([]job.Input{input}, nil, graph)
			if err != nil {
				t.Fatal(err)
			}
			_, err = instance.Plan(t.Context(), request)
			items := diagnostic.ItemsOf(err)
			if len(items) != 1 || items[0].Code != "bind.format-direct-input" || items[0].Detail["milestone"] != "M7" {
				t.Fatalf("direct input diagnostic = %#v, %v", items, err)
			}
		})
	}

	missing, err := job.New(nil, nil, graph)
	if err != nil {
		t.Fatal(err)
	}
	_, err = instance.Plan(t.Context(), missing)
	items := diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "bind.ambiguous-boundary" || items[0].Detail["choices"] != "0" || items[0].Detail["ports"] != "1" {
		t.Fatalf("missing direct input diagnostic = %#v, %v", items, err)
	}

	multipleGraph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("demux", reader.Identity(), config.NewPatch()),
			job.NewNode("reader", transform.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("demux", "packets"), job.At("sink", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := access.Parse("direct-kind:input")
	referenceInput, _ := job.InputFromReference(reference)
	multiple, err := job.New([]job.Input{referenceInput}, nil, multipleGraph)
	if err != nil {
		t.Fatal(err)
	}
	_, err = instance.Plan(t.Context(), multiple)
	items = diagnostic.ItemsOf(err)
	if len(items) != 1 || items[0].Code != "bind.ambiguous-boundary" || items[0].Detail["choices"] != "1" || items[0].Detail["ports"] != "2" {
		t.Fatalf("multiple direct input diagnostic = %#v, %v", items, err)
	}
}

func TestNormalFormatReaderStillUsesProviderCarrier(t *testing.T) {
	capabilities := mustCapabilities(t, access.SequentialRead)
	source, transform, sink, _ := boundaryComponentsWith(nil, []plugin.ComponentOption{access.Source("carrier", capabilities, boundaryAcquire(capabilities))}, nil)
	instance, err := New(Plugins(plugin.NewSet(plugin.Define[directBoundaryPluginID](plugin.Descriptor{DisplayName: "carrier boundary", Version: "1"}, source, transform, sink))))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("reader", transform.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("reader", "out"), job.At("sink", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := access.Parse("carrier:input")
	input, _ := job.InputFromReference(reference)
	request, err := job.New([]job.Input{input}, nil, graph)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := instance.Plan(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	boundaries := planned.Boundaries()
	if len(boundaries) != 1 || boundaries[0].Node == "reader" || boundaries[0].Component != source.Identity().String() {
		t.Fatalf("normal boundary projection = %#v", boundaries)
	}
	if got := len(planned.Nodes()); got != 3 {
		t.Fatalf("normal Plan nodes = %d, want provider carrier plus requested graph", got)
	}
}

var _ flow.RoutedReader[access.Write] = directBoundaryReader{}
