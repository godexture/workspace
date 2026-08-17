package host

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
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

type (
	failureDropPluginID struct{}
	failureDropConfigID struct{}
	failureDropSourceID struct{}
	failureDropSinkID   struct{}
	failureDropSchemaID struct{}
	failureDropConfig   struct{}
)

var errFailureDropSink = errors.New("sink failure")

type failureDropOperator struct{ shape flow.Shape }

func (o failureDropOperator) Ports() flow.Shape { return o.shape.Clone() }
func (failureDropOperator) Close() error        { return nil }

type failureDropReader struct {
	failureDropOperator
	typ       schema.Type[int]
	remaining int
	emitted   *atomic.Int64
}

func (r *failureDropReader) Read(_ context.Context, into *flow.Item[int]) error {
	if r.remaining == 0 {
		return io.EOF
	}
	r.remaining--
	r.emitted.Add(1)
	into.Set(r.remaining)
	return nil
}

type failureDropWriter struct {
	failureDropOperator
	failAt int
	writes int
}

func (w *failureDropWriter) Write(_ context.Context, input *flow.Item[int]) error {
	if w.writes == w.failAt {
		return errFailureDropSink
	}
	w.writes++
	input.Drop()
	return nil
}

func TestPreparedRunDropsEveryItemAcceptedFromSourceOnFailure(t *testing.T) {
	const (
		failAt = 50_000
		runs   = 40
	)
	var emitted, dropped atomic.Int64
	typ := schema.Define[failureDropSchemaID](schema.Traits[int]{Drop: func(int) { dropped.Add(1) }})
	descriptor := stream.MustDescriptor("failure-drop", typ.Descriptor(), timing.Base{}, property.New())
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	configuration := config.Struct[failureDropConfigID](func() failureDropConfig { return failureDropConfig{} }).Version("1").Build()

	source := plugin.NewComponent[failureDropSourceID](plugin.Descriptor{DisplayName: "failure source"}, configuration,
		plugin.WithSpec(plugin.Spec[failureDropConfig, flow.Shape, stream.Descriptor]{
			Shape: plugin.StaticShape[failureDropConfig](sourceShape),
			Compile: func(plugin.CompileContext, failureDropConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
				return plugin.Compiled[flow.Shape, stream.Descriptor]{Plan: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor))}, nil
			},
			Open: func(plugin.OpenContext, flow.Shape) (flow.Operator, error) {
				return &failureDropReader{
					failureDropOperator: failureDropOperator{shape: sourceShape},
					typ:                 typ,
					remaining:           failAt + 8,
					emitted:             &emitted,
				}, nil
			},
		}),
		plugin.WithReader("out", typ),
	)
	sink := plugin.NewComponent[failureDropSinkID](plugin.Descriptor{DisplayName: "failure sink"}, configuration,
		plugin.WithSpec(plugin.Spec[failureDropConfig, flow.Shape, stream.Descriptor]{
			Shape: plugin.StaticShape[failureDropConfig](sinkShape),
			Compile: func(_ plugin.CompileContext, _ failureDropConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
				if _, ok := inputs.One("in"); !ok {
					return plugin.Compiled[flow.Shape, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("failure.input"))}}, nil
				}
				return plugin.Compiled[flow.Shape, stream.Descriptor]{Plan: sinkShape, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
			},
			Open: func(plugin.OpenContext, flow.Shape) (flow.Operator, error) {
				return &failureDropWriter{failureDropOperator: failureDropOperator{shape: sinkShape}, failAt: failAt}, nil
			},
		}),
		plugin.WithWriter("in", typ),
	)
	definition := plugin.Define[failureDropPluginID](plugin.Descriptor{DisplayName: "failure drop", Version: "1"}, source, sink)
	instance, err := New(
		Plugins(plugin.NewSet(definition)),
		PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("source", source.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, graph)
	if err != nil {
		t.Fatal(err)
	}

	for run := range runs {
		beforeEmitted, beforeDropped := emitted.Load(), dropped.Load()
		result, runErr := instance.Run(context.Background(), request)
		if runErr == nil || !errors.Is(runErr, errFailureDropSink) || result.Primary == nil {
			t.Fatalf("run %d result = %#v, error = %v", run, result, runErr)
		}
		if len(result.Cleanup) != 0 {
			t.Fatalf("run %d cleanup = %#v (%v)", run, result.Cleanup, result.Cleanup[0].Err)
		}
		runEmitted := emitted.Load() - beforeEmitted
		runDropped := dropped.Load() - beforeDropped
		if runDropped != runEmitted {
			t.Fatalf("run %d dropped = %d, want %d emitted items", run, runDropped, runEmitted)
		}
	}
}

type (
	flushPhasePluginID    struct{}
	flushPhaseConfigID    struct{}
	flushPhaseSourceID    struct{}
	flushPhaseProcessorID struct{}
	flushPhaseSinkID      struct{}
	flushPhaseSchemaID    struct{}
	flushPhaseConfig      struct{}
)

var errFlushPhaseProcessor = errors.New("processor flush failed")

type flushPhaseOperator struct{ shape flow.Shape }

func (o flushPhaseOperator) Ports() flow.Shape { return o.shape.Clone() }
func (flushPhaseOperator) Close() error        { return nil }

type flushPhaseReader struct {
	flushPhaseOperator
	typ  schema.Type[int]
	read bool
}

func (r *flushPhaseReader) Read(_ context.Context, into *flow.Item[int]) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	into.Set(1)
	return nil
}

type flushPhaseProcessor struct{ flushPhaseOperator }

func (p *flushPhaseProcessor) Process(_ context.Context, input *flow.Item[int], _ flow.Emitter[int]) error {
	input.Drop()
	return nil
}

func (*flushPhaseProcessor) Flush(context.Context, flow.Emitter[int]) error {
	return errFlushPhaseProcessor
}

type flushPhaseWriter struct{ flushPhaseOperator }

func (*flushPhaseWriter) Write(_ context.Context, input *flow.Item[int]) error {
	input.Drop()
	return nil
}

// A plugin's Flush failure must land under the same phase whether a queue
// happens to sit downstream of it or not: Host's boundary heuristic (a queue
// after every source, and before every sink) puts one here regardless, so
// this alone already exercises the topology the review found inconsistent.
// The runtime attributes the failure to the operation it happened in, not to
// which goroutine noticed it.
func TestFlushFailureReportsFlushPhaseAcrossABufferedBoundary(t *testing.T) {
	typ := schema.Define[flushPhaseSchemaID](schema.Traits[int]{})
	descriptor := stream.MustDescriptor("flush-phase", typ.Descriptor(), timing.Base{}, property.New())
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ)})
	processorShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, []flow.Port{flow.Out("out", typ)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	configuration := config.Struct[flushPhaseConfigID](func() flushPhaseConfig { return flushPhaseConfig{} }).Version("1").Build()

	source := plugin.NewComponent[flushPhaseSourceID](plugin.Descriptor{DisplayName: "flush phase source"}, configuration,
		plugin.WithSpec(plugin.Spec[flushPhaseConfig, flow.Shape, stream.Descriptor]{
			Shape: plugin.StaticShape[flushPhaseConfig](sourceShape),
			Compile: func(plugin.CompileContext, flushPhaseConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
				return plugin.Compiled[flow.Shape, stream.Descriptor]{Plan: sourceShape, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor))}, nil
			},
			Open: func(plugin.OpenContext, flow.Shape) (flow.Operator, error) {
				return &flushPhaseReader{flushPhaseOperator: flushPhaseOperator{shape: sourceShape}, typ: typ}, nil
			},
		}),
		plugin.WithReader("out", typ),
	)
	processor := plugin.NewComponent[flushPhaseProcessorID](plugin.Descriptor{DisplayName: "flush phase processor"}, configuration,
		plugin.WithSpec(plugin.Spec[flushPhaseConfig, flow.Shape, stream.Descriptor]{
			Shape: plugin.StaticShape[flushPhaseConfig](processorShape),
			Compile: func(_ plugin.CompileContext, _ flushPhaseConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
				input, ok := inputs.One("in")
				if !ok {
					return plugin.Compiled[flow.Shape, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("flush-phase.input"))}}, nil
				}
				return plugin.Compiled[flow.Shape, stream.Descriptor]{Plan: processorShape, Outputs: flow.NewDescriptors(flow.Describe("out", input))}, nil
			},
			Open: func(plugin.OpenContext, flow.Shape) (flow.Operator, error) {
				return &flushPhaseProcessor{flushPhaseOperator{shape: processorShape}}, nil
			},
		}),
		plugin.WithProcessor("in", typ, "out", typ),
	)
	sink := plugin.NewComponent[flushPhaseSinkID](plugin.Descriptor{DisplayName: "flush phase sink"}, configuration,
		plugin.WithSpec(plugin.Spec[flushPhaseConfig, flow.Shape, stream.Descriptor]{
			Shape: plugin.StaticShape[flushPhaseConfig](sinkShape),
			Compile: func(_ plugin.CompileContext, _ flushPhaseConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[flow.Shape, stream.Descriptor], error) {
				if _, ok := inputs.One("in"); !ok {
					return plugin.Compiled[flow.Shape, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("flush-phase.sink"))}}, nil
				}
				return plugin.Compiled[flow.Shape, stream.Descriptor]{Plan: sinkShape, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
			},
			Open: func(plugin.OpenContext, flow.Shape) (flow.Operator, error) {
				return &flushPhaseWriter{flushPhaseOperator{shape: sinkShape}}, nil
			},
		}),
		plugin.WithWriter("in", typ),
	)
	definition := plugin.Define[flushPhasePluginID](plugin.Descriptor{DisplayName: "flush phase", Version: "1"}, source, processor, sink)
	instance, err := New(
		Plugins(plugin.NewSet(definition)),
		PlatformSnapshot(plan.Platform{OS: "test", Arch: "test", Toolchain: "go-test"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("source", source.Identity(), config.NewPatch()),
			job.NewNode("processor", processor.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("source", "out"), job.At("processor", "in")),
			job.Connect(job.At("processor", "out"), job.At("sink", "in")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, graph)
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := instance.Run(context.Background(), request)
	if runErr == nil || !errors.Is(runErr, errFlushPhaseProcessor) {
		t.Fatalf("run error = %v, want the processor's flush failure", runErr)
	}
	if result.Primary == nil || result.Primary.Phase != FlushPhase {
		t.Fatalf("primary = %#v, want FlushPhase regardless of the queue the runtime placed downstream of the processor", result.Primary)
	}
}
