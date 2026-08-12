package host

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type inspectPluginID struct{}
type inspectConfigID struct{}
type inspectSourceID struct{}
type inspectReaderID struct{}
type inspectBridgeID struct{}
type inspectSinkID struct{}
type inspectFormatID struct{}
type inspectEncodingID struct{}
type inspectCarrierID struct{}
type inspectSchemaAID struct{}
type inspectSchemaBID struct{}
type inspectUnit int
type inspectConfig struct{}

var (
	inspectSchemaA = schema.Define[inspectSchemaAID, inspectUnit](schema.Traits[inspectUnit]{})
	inspectSchemaB = schema.Define[inspectSchemaBID, inspectUnit](schema.Traits[inspectUnit]{})
)

type inspectPlan struct{ shape flow.Shape }
type inspectOperator struct{ shape flow.Shape }

func (o inspectOperator) Ports() flow.Shape { return o.shape.Clone() }
func (inspectOperator) Close() error        { return nil }

func TestPlanInspectsOnceAndReusesResultAcrossCompileFixpoints(t *testing.T) {
	slot := carrier.Define[inspectCarrierID]()
	value, err := format.Define[inspectFormatID]([]carrier.ID{slot})
	if err != nil {
		t.Fatal(err)
	}
	capabilities := mustCapabilities(t, access.RandomRead)
	sessions := &sessionCounters{}
	var inspected, compiled atomic.Int32
	configuration := config.Struct[inspectConfigID](func() inspectConfig { return inspectConfig{} }).Version("1").Build()
	component := func(shape flow.Shape, compile plugin.CompileFunc[inspectConfig, inspectPlan, stream.Descriptor]) plugin.ComponentOption {
		return plugin.WithSpec(plugin.Spec[inspectConfig, inspectPlan, stream.Descriptor]{
			Shape:   plugin.StaticShape[inspectConfig](shape),
			Compile: compile,
			Open: func(plugin.OpenContext, inspectPlan) (flow.Operator, error) {
				return inspectOperator{shape: shape}, nil
			},
		})
	}
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("bytes", access.Bytes())})
	source := plugin.NewComponent[inspectSourceID](plugin.Descriptor{DisplayName: "inspection source"}, configuration,
		component(sourceShape, func(plugin.CompileContext, inspectConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[inspectPlan, stream.Descriptor], error) {
			descriptor, descriptorErr := stream.NewDescriptor("inspect", access.Bytes().Identity(), access.CarrierTimeBase(), property.New())
			if descriptorErr != nil {
				return plugin.Compiled[inspectPlan, stream.Descriptor]{}, descriptorErr
			}
			return plugin.Compiled[inspectPlan, stream.Descriptor]{Plan: inspectPlan{shape: sourceShape}, Outputs: flow.NewDescriptors(flow.Describe("bytes", descriptor))}, nil
		}),
		plugin.WithReader("bytes", access.Bytes()),
		access.Source("inspect", capabilities, sessions.acquire(capabilities)),
	)
	readerShape := flow.NewShape([]flow.Port{flow.In("bytes", access.Bytes())}, []flow.Port{flow.Out("out", inspectSchemaA)})
	reader := plugin.NewComponent[inspectReaderID](plugin.Descriptor{DisplayName: "inspected format"}, configuration,
		component(readerShape, func(ctx plugin.CompileContext, _ inspectConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[inspectPlan, stream.Descriptor], error) {
			compiled.Add(1)
			if resolver, ok := metadata.ResolverOf(ctx); !ok || !resolver.Valid() {
				return plugin.Compiled[inspectPlan, stream.Descriptor]{}, errors.New("Compile did not receive the prepared metadata resolver")
			}
			prepared, ok := format.InspectionOf[int](ctx, value)
			if !ok || prepared != 44 || inspected.Load() != 1 {
				return plugin.Compiled[inspectPlan, stream.Descriptor]{}, errors.New("Compile did not receive the one prepared inspection")
			}
			input, ok := inputs.One("bytes")
			if !ok {
				return plugin.Compiled[inspectPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("inspect.input"))}}, nil
			}
			output := stream.MustDescriptor(input.ID(), inspectSchemaA.Identity(), timing.MustBase(1, 1), property.New()).WithMetadata(input.Metadata())
			return plugin.Compiled[inspectPlan, stream.Descriptor]{Plan: inspectPlan{shape: readerShape}, Outputs: flow.NewDescriptors(flow.Describe("out", output)), Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "inspect"}}}, nil
		}),
		plugin.WithProcessor("bytes", access.Bytes(), "out", inspectSchemaA),
		format.Read(value, access.NewRequirements(access.AllOf(access.RandomRead)), format.WithInspect(func(ctx format.InspectContext) (format.Inspection, error) {
			if _, ok := access.RandomOf(ctx.Opening()); !ok {
				return format.Inspection{}, errors.New("Inspect did not receive the selected Random view")
			}
			resolver, ok := metadata.ResolverOf(ctx.Prepared())
			if !ok {
				return format.Inspection{}, errors.New("Inspect did not receive the prepared metadata resolver")
			}
			if _, err := resolver.Parse(ctx.Context(), slot, "inspect", metadata.StreamScope, metadata.NewBlob("", nil)); err != nil {
				return format.Inspection{}, err
			}
			inspected.Add(1)
			return format.NewInspection(value, 44), nil
		})),
	)
	bridgeShape := flow.NewShape([]flow.Port{flow.In("in", inspectSchemaA)}, []flow.Port{flow.Out("out", inspectSchemaB)})
	bridge := plugin.NewComponent[inspectBridgeID](plugin.Descriptor{DisplayName: "inspection bridge"}, configuration,
		component(bridgeShape, func(_ plugin.CompileContext, _ inspectConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[inspectPlan, stream.Descriptor], error) {
			input, ok := inputs.One("in")
			if !ok {
				return plugin.Compiled[inspectPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("bridge.input"))}}, nil
			}
			output := stream.MustDescriptor(input.ID(), inspectSchemaB.Identity(), input.TimeBase(), input.Properties()).WithMetadata(input.Metadata())
			return plugin.Compiled[inspectPlan, stream.Descriptor]{Plan: inspectPlan{shape: bridgeShape}, Outputs: flow.NewDescriptors(flow.Describe("out", output)), Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "bridge"}}}, nil
		}),
		plugin.WithProcessor("in", inspectSchemaA, "out", inspectSchemaB),
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", inspectSchemaB)}, nil)
	sink := plugin.NewComponent[inspectSinkID](plugin.Descriptor{DisplayName: "inspection sink"}, configuration,
		component(sinkShape, func(_ plugin.CompileContext, _ inspectConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[inspectPlan, stream.Descriptor], error) {
			if _, ok := inputs.One("in"); !ok {
				return plugin.Compiled[inspectPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("sink.input"))}}, nil
			}
			return plugin.Compiled[inspectPlan, stream.Descriptor]{Plan: inspectPlan{shape: sinkShape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
		}),
		plugin.WithWriter("in", inspectSchemaB),
	)
	encoding := plugin.NewComponent[inspectEncodingID](plugin.Descriptor{DisplayName: "inspection metadata encoding"}, configuration,
		metadata.WithEncoding(
			func(ctx metadata.ParseContext) (metadata.Document, error) {
				if ctx.Carrier() != slot || ctx.Encoding() != plugin.IdentityOf[inspectEncodingID]() {
					return metadata.Document{}, errors.New("metadata resolver selected the wrong encoding")
				}
				return metadata.NewBuilder(ctx.Scope()).Build()
			},
			func(metadata.MarshalContext) (metadata.Blob, error) { return metadata.NewBlob("", nil), nil },
		),
	)
	set := plugin.NewSet(plugin.Define[inspectPluginID](plugin.Descriptor{DisplayName: "inspection", Version: "1"}, source, reader, bridge, sink, encoding)).AddDeclaration(metadata.Bind(slot, encoding.Identity()))
	instance, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	requested, err := job.NewGraph(
		[]job.Node{
			job.NewNode("reader", reader.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("reader", "out"), job.At("sink", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := access.Parse("inspect:input")
	input, _ := job.InputFromReference(reference)
	request, err := job.New([]job.Input{input}, nil, requested)
	if err != nil {
		t.Fatal(err)
	}
	public, err := instance.Plan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !public.Valid() || inspected.Load() != 1 || compiled.Load() < 2 {
		t.Fatalf("Plan valid=%v, Inspect=%d, Compile=%d", public.Valid(), inspected.Load(), compiled.Load())
	}
	if sessions.acquired.Load() != 1 || sessions.closed.Load() != 1 {
		t.Fatalf("input sessions = %d/%d", sessions.acquired.Load(), sessions.closed.Load())
	}
}

func TestPlanDiagnosesDirectSourceRequiringFormatInspection(t *testing.T) {
	source, reader, sink, _ := boundaryComponentsWith(nil, nil, nil, format.WithInspect(func(format.InspectContext) (format.Inspection, error) {
		return format.NewInspection(boundaryFormat(), struct{}{}), nil
	}))
	set := plugin.NewSet(plugin.Define[boundaryPluginID](
		plugin.Descriptor{DisplayName: "direct inspection fixture", Version: "1"},
		source,
		reader,
		sink,
	))
	instance, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	adaptor, err := job.NewAdaptor(source.Identity(), config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	input, err := job.InputFromSource(access.Borrow(&lifecycleHandle{}), adaptor)
	if err != nil {
		t.Fatal(err)
	}
	requested, err := job.NewGraph(
		[]job.Node{
			job.NewNode("reader", reader.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("reader", "out"), job.At("sink", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, nil, requested)
	if err != nil {
		t.Fatal(err)
	}

	_, err = instance.Plan(context.Background(), request)
	if err == nil {
		t.Fatal("Plan accepted a direct source requiring Format Inspect")
	}
	items := Diagnostics(err)
	if len(items) != 1 || items[0].Code != "prepare.inspect-direct" || items[0].Detail["milestone"] != "M9" || items[0].Detail["boundary"] == "" || items[0].Detail["formatNode"] != reader.Identity().String() {
		t.Fatalf("diagnostics = %#v", items)
	}
}
