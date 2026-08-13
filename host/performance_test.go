package host

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

const performanceItems = 32 << 10

type (
	performancePluginID    struct{}
	performanceConfigID    struct{}
	performanceSchemaID    struct{}
	performanceSourceID    struct{}
	performanceProcessorID struct{}
	performanceSinkID      struct{}
	performanceConfig      struct{}
)

var performanceSchema = schema.Define[performanceSchemaID](schema.Traits[int]{})

type performancePlan struct{ shape flow.Shape }

type performanceOperator struct{ shape flow.Shape }

func (o performanceOperator) Ports() flow.Shape { return o.shape.Clone() }
func (performanceOperator) Close() error        { return nil }

type performanceReader struct {
	performanceOperator
	remaining int
}

func (r *performanceReader) Read(_ context.Context, into *flow.Item[int]) error {
	if r.remaining == 0 {
		return io.EOF
	}
	value := performanceItems - r.remaining
	r.remaining--
	*into = flow.NewItem(value, performanceSchema)
	return nil
}

type performanceProcessor struct{ performanceOperator }

func (*performanceProcessor) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	item := flow.NewItem(input.Value()+1, performanceSchema)
	if err := output.Emit(ctx, &item); err != nil {
		item.Drop()
		return err
	}
	input.Drop()
	return nil
}

func (*performanceProcessor) Flush(context.Context, flow.Emitter[int]) error { return nil }

type performanceState struct {
	count int
	sum   int64
}

type performanceWriter struct {
	performanceOperator
	state *performanceState
}

func (w *performanceWriter) Write(_ context.Context, input *flow.Item[int]) error {
	w.state.count++
	w.state.sum += int64(input.Value())
	input.Drop()
	return nil
}

func performanceFixture() (*Host, job.Job, *performanceState, error) {
	descriptor := stream.MustDescriptor("performance", performanceSchema.Identity(), timing.MustBase(1, 1_000), property.New())
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", performanceSchema)})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", performanceSchema)}, []flow.Port{flow.Out("out", performanceSchema)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", performanceSchema)}, nil)
	configuration := config.Struct[performanceConfigID](func() performanceConfig { return performanceConfig{} }).Version("1").Build()
	state := &performanceState{}

	source := plugin.NewComponent[performanceSourceID](plugin.Descriptor{DisplayName: "performance source"}, configuration, plugin.WithSpec(plugin.Spec[performanceConfig, performancePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[performanceConfig](sourceShape),
		Compile: func(plugin.CompileContext, performanceConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[performancePlan, stream.Descriptor], error) {
			return plugin.Compiled[performancePlan, stream.Descriptor]{Plan: performancePlan{shape: sourceShape}, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor))}, nil
		},
		Open: func(plugin.OpenContext, performancePlan) (flow.Operator, error) {
			return &performanceReader{performanceOperator: performanceOperator{shape: sourceShape}, remaining: performanceItems}, nil
		},
	}), plugin.WithReader("out", performanceSchema))

	processor := plugin.NewComponent[performanceProcessorID](plugin.Descriptor{DisplayName: "performance processor"}, configuration, plugin.WithSpec(plugin.Spec[performanceConfig, performancePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[performanceConfig](processorShape),
		Compile: func(_ plugin.CompileContext, _ performanceConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[performancePlan, stream.Descriptor], error) {
			input, ok := inputs.One("in")
			if !ok {
				return plugin.Compiled[performancePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("performance.input"))}}, nil
			}
			return plugin.Compiled[performancePlan, stream.Descriptor]{Plan: performancePlan{shape: processorShape}, Outputs: flow.NewDescriptors(flow.Describe("out", input))}, nil
		},
		Open: func(plugin.OpenContext, performancePlan) (flow.Operator, error) {
			return &performanceProcessor{performanceOperator{shape: processorShape}}, nil
		},
	}), plugin.WithProcessor("in", performanceSchema, "out", performanceSchema))

	sink := plugin.NewComponent[performanceSinkID](plugin.Descriptor{DisplayName: "performance sink"}, configuration, plugin.WithSpec(plugin.Spec[performanceConfig, performancePlan, stream.Descriptor]{
		Shape: plugin.StaticShape[performanceConfig](sinkShape),
		Compile: func(_ plugin.CompileContext, _ performanceConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[performancePlan, stream.Descriptor], error) {
			if _, ok := inputs.One("in"); !ok {
				return plugin.Compiled[performancePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("performance.input"))}}, nil
			}
			return plugin.Compiled[performancePlan, stream.Descriptor]{Plan: performancePlan{shape: sinkShape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
		},
		Open: func(plugin.OpenContext, performancePlan) (flow.Operator, error) {
			return &performanceWriter{performanceOperator: performanceOperator{shape: sinkShape}, state: state}, nil
		},
	}), plugin.WithWriter("in", performanceSchema))

	definition := plugin.Define[performancePluginID](plugin.Descriptor{DisplayName: "performance", Version: "1"}, source, processor, sink)
	host, err := New(Plugins(plugin.NewSet(definition)), PlatformSnapshot(plan.Platform{OS: "benchmark", Arch: "benchmark", Toolchain: "go-test"}))
	if err != nil {
		return nil, job.Job{}, nil, err
	}
	graph, err := job.NewGraph([]job.Node{
		job.NewNode("source", source.Identity(), config.NewPatch()),
		job.NewNode("first", processor.Identity(), config.NewPatch()),
		job.NewNode("second", processor.Identity(), config.NewPatch()),
		job.NewNode("sink", sink.Identity(), config.NewPatch()),
	}, []job.Edge{
		job.Connect(job.At("source", "out"), job.At("first", "in")),
		job.Connect(job.At("first", "out"), job.At("second", "in")),
		job.Connect(job.At("second", "out"), job.At("sink", "in")),
	})
	if err != nil {
		return nil, job.Job{}, nil, err
	}
	request, err := job.New(nil, nil, graph)
	return host, request, state, err
}

func BenchmarkPreparedRunLinear(b *testing.B) {
	host, request, state, err := performanceFixture()
	if err != nil {
		b.Fatal(err)
	}
	wantSum := int64(performanceItems*(performanceItems-1)/2 + performanceItems*2)
	b.ReportAllocs()
	b.SetBytes(performanceItems * 8)
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		state.count, state.sum = 0, 0
		prepared, err := host.Prepare(context.Background(), request)
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		result, runErr := prepared.Run(context.Background())
		b.StopTimer()
		closeErr := prepared.Close()
		if runErr != nil || closeErr != nil || !result.Succeeded() {
			b.Fatal(fmt.Errorf("Host run failed: run=%w close=%v result=%#v", runErr, closeErr, result))
		}
		if state.count != performanceItems || state.sum != wantSum {
			b.Fatalf("result = count %d sum %d, want count %d sum %d", state.count, state.sum, performanceItems, wantSum)
		}
	}
}
