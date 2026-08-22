package host

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
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

type preparedSourcePluginID struct{}
type preparedSourceAccessID struct{}
type preparedSourceReaderID struct{}
type preparedSourceWriterID struct{}
type preparedSourceSinkID struct{}
type preparedSourceAccessSinkID struct{}

type preparedSourceSession struct {
	data   []byte
	closed *atomic.Int32
}

func (s *preparedSourceSession) Capabilities() access.Capabilities {
	return mustCapabilitiesValue(access.RandomRead, access.StableSize)
}

func (s *preparedSourceSession) ReadAt(_ context.Context, destination []byte, offset int64) (int, error) {
	if s.closed.Load() != 0 {
		return 0, errors.New("read after source session close")
	}
	if offset < 0 || offset >= int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(destination, s.data[offset:])
	if n != len(destination) {
		return n, io.EOF
	}
	return n, nil
}

func (s *preparedSourceSession) Size(context.Context) (int64, error) {
	return int64(len(s.data)), nil
}

func (*preparedSourceSession) Snapshot(context.Context) (access.Snapshot, error) {
	return access.NewSnapshot("host/test/prepared-source", access.StrongSnapshot)
}

func (s *preparedSourceSession) Close() error {
	s.closed.Add(1)
	return nil
}

type preparedSourceReader struct{ shape flow.Shape }

func (o preparedSourceReader) Ports() flow.Shape { return o.shape.Clone() }
func (preparedSourceReader) Close() error        { return nil }
func (preparedSourceReader) Process(_ context.Context, input *flow.Item[buffer.Handle], _ flow.Emitter[inspectUnit]) error {
	input.Drop()
	return nil
}
func (preparedSourceReader) Flush(context.Context, flow.Emitter[inspectUnit]) error { return nil }

type preparedSourceWriter struct{ shape flow.Shape }

func (o preparedSourceWriter) Ports() flow.Shape { return o.shape.Clone() }
func (preparedSourceWriter) Close() error        { return nil }
func (preparedSourceWriter) Process(_ context.Context, input *flow.Item[inspectUnit], _ flow.Emitter[access.Write]) error {
	input.Drop()
	return nil
}
func (preparedSourceWriter) Flush(context.Context, flow.Emitter[access.Write]) error { return nil }

func TestPreparedFormatSourceOpeningUsesOriginalSession(t *testing.T) {
	capabilities := mustCapabilitiesValue(access.RandomRead, access.StableSize)
	data := []byte("0123456789abcdef")
	var acquired atomic.Int32
	var sessions []*atomic.Int32
	acquire := func(context.Context, access.Reference, access.Selection) (access.Session, error) {
		acquired.Add(1)
		closed := &atomic.Int32{}
		sessions = append(sessions, closed)
		return &preparedSourceSession{data: data, closed: closed}, nil
	}
	configuration := config.Struct[boundaryConfigID](func() boundaryConfig { return boundaryConfig{} }).Version("1").Build()

	var componentOpens atomic.Int32
	var limited access.Opening
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", access.Bytes())})
	source := plugin.NewComponent[preparedSourceAccessID](plugin.Descriptor{DisplayName: "prepared source access"}, configuration,
		plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Ports: sourceShape,
			Compile: func(plugin.CompileContext, boundaryConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
				descriptor := stream.MustDescriptor("source", access.Bytes().Descriptor(), timing.Base{}, property.New())
				return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: sourceShape}, Outputs: flow.NewDescriptors(flow.Describe("out", descriptor))}, nil
			},
			Open: func(ctx plugin.OpenContext, _ boundaryPlan) (flow.Operator, error) {
				componentOpens.Add(1)
				if _, ok := mediaformat.SourceOpening(ctx); ok {
					return nil, errors.New("Access boundary received a Format source opening")
				}
				return boundarySourceOperator{boundaryOperator{shape: sourceShape}}, nil
			},
		}),
		plugin.WithReader("out", access.Bytes()),
		access.Source("prepared", capabilities, acquire),
	)

	checkSource := func(ctx plugin.OpenContext) error {
		opening, ok := mediaformat.SourceOpening(ctx)
		if !ok {
			return errors.New("Format Open did not receive its prepared source")
		}
		view, ok := access.RandomOf(opening)
		if !ok {
			return errors.New("prepared source has no RandomRead view")
		}
		read := make([]byte, 4)
		if err := access.ReadFullAt(ctx.Context(), view, read, 8); err != nil {
			return err
		}
		if string(read) != "89ab" {
			return errors.New("prepared source returned the wrong bytes")
		}
		limitedView, ok := access.RandomOf(limited)
		if !ok {
			return errors.New("Inspect opening was not retained by the test")
		}
		if err := access.ReadFullAt(ctx.Context(), limitedView, make([]byte, 1), 8); err == nil {
			return errors.New("Inspect opening escaped its byte limit")
		}
		return nil
	}

	readerShape := flow.NewShape([]flow.Port{flow.In("bytes", access.Bytes())}, []flow.Port{flow.Out("out", inspectSchemaA)})
	reader := plugin.NewComponent[preparedSourceReaderID](plugin.Descriptor{DisplayName: "prepared source reader"}, configuration,
		plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Ports: readerShape,
			Compile: func(_ plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
				input, ok := inputs.One("bytes")
				if !ok {
					return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("bytes", plugin.ConditionNeed[stream.Descriptor]("source.input"))}}, nil
				}
				output := stream.MustDescriptor(input.ID(), inspectSchemaA.Descriptor(), timing.MustBase(1, 1), property.New())
				return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: readerShape}, Outputs: flow.NewDescriptors(flow.Describe("out", output))}, nil
			},
			Open: func(ctx plugin.OpenContext, _ boundaryPlan) (flow.Operator, error) {
				componentOpens.Add(1)
				if err := checkSource(ctx); err != nil {
					return nil, err
				}
				return preparedSourceReader{shape: readerShape}, nil
			},
		}),
		plugin.WithProcessor("bytes", access.Bytes(), "out", inspectSchemaA),
		mediaformat.Read(boundaryFormat(), access.NewRequirements(access.AllOf(access.RandomRead, access.StableSize)), mediaformat.WithInspect(func(ctx mediaformat.InspectContext) (mediaformat.Inspection, error) {
			limited = ctx.Opening()
			view, ok := access.RandomOf(limited)
			if !ok {
				return mediaformat.Inspection{}, errors.New("Inspect has no RandomRead view")
			}
			if err := access.ReadFullAt(ctx.Context(), view, make([]byte, 4), 0); err != nil {
				return mediaformat.Inspection{}, err
			}
			return mediaformat.NewInspection(boundaryFormat(), 1), nil
		})),
	)

	writerShape := flow.NewShape([]flow.Port{flow.In("in", inspectSchemaA)}, []flow.Port{flow.Out("writes", access.Writes())})
	writer := plugin.NewComponent[preparedSourceWriterID](plugin.Descriptor{DisplayName: "prepared source writer"}, configuration,
		plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Ports: writerShape,
			Compile: func(ctx plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
				if _, ok := mediaformat.InspectionOf[int](ctx, boundaryFormat()); !ok {
					return plugin.Compiled[boundaryPlan, stream.Descriptor]{}, errors.New("writer did not receive the source inspection")
				}
				input, ok := inputs.One("in")
				if !ok {
					return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("writer.input"))}}, nil
				}
				output := stream.MustDescriptor(input.ID(), access.Writes().Descriptor(), timing.Base{}, property.New())
				return plugin.Compiled[boundaryPlan, stream.Descriptor]{
					Plan:    boundaryPlan{shape: writerShape},
					Outputs: flow.NewDescriptors(flow.Describe("writes", output)),
					Effects: []plugin.Effect{{Kind: plugin.StructuralEffect, Loss: plugin.NoLoss, Detail: "write format"}},
				}, nil
			},
			Open: func(ctx plugin.OpenContext, _ boundaryPlan) (flow.Operator, error) {
				componentOpens.Add(1)
				if err := checkSource(ctx); err != nil {
					return nil, err
				}
				return preparedSourceWriter{shape: writerShape}, nil
			},
		}),
		plugin.WithProcessor("in", inspectSchemaA, "writes", access.Writes()),
		mediaformat.Write(boundaryFormat(), access.NewRequirements(access.AllOf(access.SequentialWrite))),
	)

	sinkShape := flow.NewShape([]flow.Port{flow.In("writes", access.Writes())}, nil)
	sink := plugin.NewComponent[preparedSourceSinkID](plugin.Descriptor{DisplayName: "prepared source sink"}, configuration,
		plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Ports: sinkShape,
			Compile: func(_ plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
				if _, ok := inputs.One("writes"); !ok {
					return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("writes", plugin.ConditionNeed[stream.Descriptor]("sink.input"))}}, nil
				}
				return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: sinkShape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
			},
			Open: func(ctx plugin.OpenContext, _ boundaryPlan) (flow.Operator, error) {
				componentOpens.Add(1)
				if _, ok := mediaformat.SourceOpening(ctx); ok {
					return nil, errors.New("Endpoint boundary received a Format source opening")
				}
				return boundarySinkOperator{boundaryOperator{shape: sinkShape}}, nil
			},
		}),
		plugin.WithWriter("writes", access.Writes()),
		endpoint.WithTrait(mustEndpointTrait()),
	)

	sinkCapabilities := mustCapabilitiesValue(access.SequentialWrite)
	outputSessions := &sessionCounters{}
	accessSink := plugin.NewComponent[preparedSourceAccessSinkID](plugin.Descriptor{DisplayName: "prepared source access sink"}, configuration,
		plugin.WithSpec(plugin.Spec[boundaryConfig, boundaryPlan, stream.Descriptor]{
			Ports: sinkShape,
			Compile: func(_ plugin.CompileContext, _ boundaryConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[boundaryPlan, stream.Descriptor], error) {
				if _, ok := inputs.One("writes"); !ok {
					return plugin.Compiled[boundaryPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("writes", plugin.ConditionNeed[stream.Descriptor]("access-sink.input"))}}, nil
				}
				return plugin.Compiled[boundaryPlan, stream.Descriptor]{Plan: boundaryPlan{shape: sinkShape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
			},
			Open: func(ctx plugin.OpenContext, _ boundaryPlan) (flow.Operator, error) {
				componentOpens.Add(1)
				if _, ok := mediaformat.SourceOpening(ctx); ok {
					return nil, errors.New("Access output boundary received a Format source opening")
				}
				return boundarySinkOperator{boundaryOperator{shape: sinkShape}}, nil
			},
		}),
		plugin.WithWriter("writes", access.Writes()),
		access.Sink("prepared-out", sinkCapabilities, access.AtomicReplace, outputSessions.acquire(sinkCapabilities)),
	)

	set := plugin.NewSet(plugin.Define[preparedSourcePluginID](plugin.Descriptor{DisplayName: "prepared source", Version: "1"}, source, reader, writer, sink, accessSink))
	instance, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := job.NewGraph(
		[]job.Node{
			job.NewNode("reader", reader.Identity(), config.NewPatch()),
			job.NewNode("writer", writer.Identity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("reader", "out"), job.At("writer", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	reference, _ := access.Parse("prepared:input")
	input, _ := job.InputFromReference(reference)
	input, err = input.WithFormatHint(mustFormatSelector(t, boundaryFormat()))
	if err != nil {
		t.Fatal(err)
	}
	requestValue, _ := job.NewEndpoint(sink.Identity(), config.NewPatch())
	output, _ := job.OutputToEndpoint(requestValue)
	budget := job.DefaultBudget()
	budget.InspectBytes = 4
	request, err := job.New([]job.Input{input}, []job.Output{output}, graph, job.WithBudget(budget))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := instance.Plan(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if componentOpens.Load() != 0 || acquired.Load() != 1 || len(sessions) != 1 || sessions[0].Load() != 1 {
		t.Fatalf("Plan lifecycle: opens=%d acquired=%d sessions=%d closed=%d", componentOpens.Load(), acquired.Load(), len(sessions), sessions[0].Load())
	}
	if _, err := instance.Run(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if componentOpens.Load() != 4 || acquired.Load() != 2 || len(sessions) != 2 || sessions[1].Load() != 1 {
		t.Fatalf("Run lifecycle: opens=%d acquired=%d sessions=%d closed=%d", componentOpens.Load(), acquired.Load(), len(sessions), sessions[1].Load())
	}

	automaticGraph, err := job.NewGraph([]job.Node{job.NewNode("reader", reader.Identity(), config.NewPatch())}, nil)
	if err != nil {
		t.Fatal(err)
	}
	outputReference, _ := access.Parse("prepared-out:output")
	automaticOutput, _ := job.OutputToReference(outputReference)
	automaticRequest, err := job.New([]job.Input{input}, []job.Output{automaticOutput}, automaticGraph, job.WithBudget(budget))
	if err != nil {
		t.Fatal(err)
	}
	automaticPlan, err := instance.Plan(t.Context(), automaticRequest)
	if err != nil {
		t.Fatal(err)
	}
	automaticWriters := 0
	for _, node := range automaticPlan.Nodes() {
		if node.Component == writer.Identity().String() && node.Origin == plan.Automatic {
			automaticWriters++
		}
	}
	if automaticWriters != 1 {
		t.Fatalf("automatic Format writers = %d", automaticWriters)
	}
	if componentOpens.Load() != 4 || acquired.Load() != 3 || len(sessions) != 3 || sessions[2].Load() != 1 || outputSessions.acquired.Load() != 0 {
		t.Fatalf("automatic Plan lifecycle: opens=%d acquired=%d sessions=%d closed=%d outputs=%d", componentOpens.Load(), acquired.Load(), len(sessions), sessions[2].Load(), outputSessions.acquired.Load())
	}
	if _, err := instance.Run(t.Context(), automaticRequest); err != nil {
		t.Fatal(err)
	}
	if componentOpens.Load() != 8 || acquired.Load() != 4 || len(sessions) != 4 || sessions[3].Load() != 1 || outputSessions.acquired.Load() != 1 || outputSessions.closed.Load() != 1 {
		t.Fatalf("automatic Run lifecycle: opens=%d acquired=%d sessions=%d closed=%d outputs=%d/%d", componentOpens.Load(), acquired.Load(), len(sessions), sessions[3].Load(), outputSessions.acquired.Load(), outputSessions.closed.Load())
	}
}

func mustCapabilitiesValue(values ...access.Capability) access.Capabilities {
	result, err := access.NewCapabilities(values...)
	if err != nil {
		panic(err)
	}
	return result
}

func mustEndpointTrait() endpoint.Trait {
	result, err := endpoint.NewTrait(endpoint.FiniteStatic, endpoint.Offline)
	if err != nil {
		panic(err)
	}
	return result
}

func mustFormatSelector(t testing.TB, value mediaformat.Format) job.FormatSelector {
	t.Helper()
	result, err := job.SelectFormat(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
