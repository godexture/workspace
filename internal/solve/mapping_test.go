package solve

import (
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type mappingReaderID struct{}
type mappingWriterID struct{}
type mappingReorderID struct{}
type mappingSinkID struct{}
type mappingFormatID struct{}

func TestProjectMappingsUsesCompiledReaderOrder(t *testing.T) {
	format, reader, writer, sink := mappingComponents(t, []stream.ID{"video", "audio"})
	requested := mappingRequest(t, reader, writer, sink, nil,
		job.MapStream(0, "audio", 0),
		job.MapStream(0, "video", 0),
	)
	contexts := mappingContexts(t, format, "reader", "audio", "video")
	projected, err := evaluateMappings(t, requested, contexts, reader, writer, sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 2 || projected[0].Stream != "video" || projected[1].Stream != "audio" {
		t.Fatalf("projected mappings = %#v", projected)
	}
}

func TestProjectMappingsProjectsAllReaderStreamsWithoutSelection(t *testing.T) {
	_, reader, writer, sink := mappingComponents(t, []stream.ID{"video", "audio"})
	requested := mappingRequest(t, reader, writer, sink, nil)
	projected, err := evaluateMappings(t, requested, graph.CompileContexts{}, reader, writer, sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 2 || projected[0].Stream != "video" || projected[1].Stream != "audio" {
		t.Fatalf("preserve-all mappings = %#v", projected)
	}
}

func TestProjectMappingsRejectsReaderThatDoesNotHonorSelection(t *testing.T) {
	tests := []struct {
		name      string
		outputs   []stream.ID
		selection []stream.ID
		mapping   stream.ID
	}{
		{name: "nonexistent", outputs: []stream.ID{"video"}, selection: []stream.ID{"missing"}, mapping: "missing"},
		{name: "ignored with extra output", outputs: []stream.ID{"video", "audio"}, selection: []stream.ID{"audio"}, mapping: "audio"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			format, reader, writer, sink := mappingComponents(t, test.outputs)
			requested := mappingRequest(t, reader, writer, sink, nil, job.MapStream(0, test.mapping, 0))
			contexts := mappingContexts(t, format, "reader", test.selection...)
			_, err := evaluateMappings(t, requested, contexts, reader, writer, sink)
			if !hasDiagnostic(err, "solve.mapping-selection") {
				t.Fatalf("mapping error = %v", err)
			}
		})
	}
}

func TestProjectMappingsRejectsWriterInputReordering(t *testing.T) {
	format, reader, writer, sink := mappingComponents(t, []stream.ID{"video", "audio"})
	reorder := mappingReorderComponent()
	requested := mappingRequest(t, reader, writer, sink, &reorder,
		job.MapStream(0, "audio", 0),
		job.MapStream(0, "video", 0),
	)
	contexts := mappingContexts(t, format, "reader", "audio", "video")
	_, err := evaluateMappings(t, requested, contexts, reader, writer, sink, reorder)
	if !hasDiagnostic(err, "solve.mapping-writer") {
		t.Fatalf("mapping error = %v", err)
	}
}

func TestMappingStreamsRejectsDuplicateIDs(t *testing.T) {
	descriptor := mappingDescriptor("audio")
	if _, err := mappingStreams([]stream.Descriptor{descriptor, descriptor}); err == nil {
		t.Fatal("duplicate mapping stream was accepted")
	}
}

func TestNewPreselectionRejectsDuplicateFormatBoundary(t *testing.T) {
	_, err := NewPreselection(nil, nil, []SelectedFormat{
		{Direction: plan.InputBoundary, Choice: 0, Node: "first"},
		{Direction: plan.InputBoundary, Choice: 0, Node: "second"},
	}, nil, plan.Usage{})
	if err == nil {
		t.Fatal("duplicate selected Format boundary was accepted")
	}
}

func mappingComponents(t testing.TB, outputs []stream.ID) (mediaformat.Format, plugin.Component, plugin.Component, plugin.Component) {
	t.Helper()
	format, err := mediaformat.DefinePacketized[mappingFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	readerShape := flow.NewShape(nil, []flow.Port{flow.Out("streams", solveSchemaA, flow.Many())})
	readerSpec := plugin.Spec[solveConfig, solvePlan, stream.Descriptor]{
		Ports: readerShape,
		Compile: func(plugin.CompileContext, solveConfig, flow.Descriptors[stream.Descriptor]) (plugin.Compiled[solvePlan, stream.Descriptor], error) {
			bindings := make([]flow.PortDescriptor[stream.Descriptor], len(outputs))
			for index, id := range outputs {
				bindings[index] = flow.Describe("streams", mappingDescriptor(id))
			}
			return plugin.Compiled[solvePlan, stream.Descriptor]{Plan: solvePlan{shape: readerShape}, Outputs: flow.NewDescriptors(bindings...)}, nil
		},
		Open: func(plugin.OpenContext, solvePlan) (flow.Operator, error) {
			return solveOperator{shape: readerShape}, nil
		},
	}
	reader := plugin.NewComponent[mappingReaderID](plugin.Descriptor{DisplayName: "mapping reader"}, solveConfigSchema(),
		plugin.WithSpec(readerSpec),
		mediaformat.Read(format, access.NewRequirements(access.AllOf(access.RandomRead))),
	)

	writerShape := flow.NewShape(
		[]flow.Port{flow.In("streams", solveSchemaA, flow.Many(), flow.WithFanIn(flow.SerialFanIn))},
		[]flow.Port{flow.Out("writes", access.Writes())},
	)
	writerSpec := plugin.Spec[solveConfig, solvePlan, stream.Descriptor]{
		Ports: writerShape,
		Compile: func(_ plugin.CompileContext, _ solveConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[solvePlan, stream.Descriptor], error) {
			if len(inputs.At("streams")) == 0 {
				return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("streams", plugin.ConditionNeed[stream.Descriptor]("mapping.input"))}}, nil
			}
			output := stream.MustDescriptor("writes", access.Writes().Descriptor(), timing.Base{}, property.New())
			return plugin.Compiled[solvePlan, stream.Descriptor]{Plan: solvePlan{shape: writerShape}, Outputs: flow.NewDescriptors(flow.Describe("writes", output))}, nil
		},
		Open: func(plugin.OpenContext, solvePlan) (flow.Operator, error) {
			return solveOperator{shape: writerShape}, nil
		},
	}
	writer := plugin.NewComponent[mappingWriterID](plugin.Descriptor{DisplayName: "mapping writer"}, solveConfigSchema(),
		plugin.WithSpec(writerSpec),
		mediaformat.Write(format, access.NewRequirements(access.AllOf(access.RandomWrite))),
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", access.Writes())}, nil)
	sinkSpec := plugin.Spec[solveConfig, solvePlan, stream.Descriptor]{
		Ports: sinkShape,
		Compile: func(_ plugin.CompileContext, _ solveConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[solvePlan, stream.Descriptor], error) {
			if _, ok := inputs.One("in"); !ok {
				return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("mapping.sink"))}}, nil
			}
			return plugin.Compiled[solvePlan, stream.Descriptor]{Plan: solvePlan{shape: sinkShape}, Outputs: flow.NewDescriptors[stream.Descriptor]()}, nil
		},
		Open: func(plugin.OpenContext, solvePlan) (flow.Operator, error) {
			return solveOperator{shape: sinkShape}, nil
		},
	}
	sink := plugin.NewComponent[mappingSinkID](plugin.Descriptor{DisplayName: "mapping sink"}, solveConfigSchema(), plugin.WithSpec(sinkSpec))
	return format, reader, writer, sink
}

func mappingReorderComponent() plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("in", solveSchemaA, flow.Many(), flow.WithFanIn(flow.SerialFanIn))},
		[]flow.Port{flow.Out("out", solveSchemaA, flow.Many())},
	)
	spec := plugin.Spec[solveConfig, solvePlan, stream.Descriptor]{
		Ports: shape,
		Compile: func(_ plugin.CompileContext, _ solveConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[solvePlan, stream.Descriptor], error) {
			values := inputs.At("in")
			if len(values) == 0 {
				return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("mapping.input"))}}, nil
			}
			bindings := make([]flow.PortDescriptor[stream.Descriptor], len(values))
			for index := range values {
				bindings[index] = flow.Describe("out", values[len(values)-1-index])
			}
			return plugin.Compiled[solvePlan, stream.Descriptor]{Plan: solvePlan{shape: shape}, Outputs: flow.NewDescriptors(bindings...)}, nil
		},
		Open: func(plugin.OpenContext, solvePlan) (flow.Operator, error) { return solveOperator{shape: shape}, nil },
	}
	return plugin.NewComponent[mappingReorderID](plugin.Descriptor{DisplayName: "mapping reorder"}, solveConfigSchema(), plugin.WithSpec(spec))
}

func mappingRequest(t testing.TB, reader, writer, sink plugin.Component, reorder *plugin.Component, mappings ...job.Mapping) job.Job {
	t.Helper()
	nodes := []job.Node{
		job.NewNode("reader", reader.Identity(), config.NewPatch()),
		job.NewNode("writer", writer.Identity(), config.NewPatch()),
		job.NewNode("sink", sink.Identity(), config.NewPatch()),
	}
	edges := []job.Edge{
		job.Connect(job.At("reader", "streams"), job.At("writer", "streams")),
		job.Connect(job.At("writer", "writes"), job.At("sink", "in")),
	}
	if reorder != nil {
		nodes = append(nodes, job.NewNode("reorder", reorder.Identity(), config.NewPatch()))
		edges = []job.Edge{
			job.Connect(job.At("reader", "streams"), job.At("reorder", "in")),
			job.Connect(job.At("reorder", "out"), job.At("writer", "streams")),
			job.Connect(job.At("writer", "writes"), job.At("sink", "in")),
		}
	}
	requested, err := job.NewGraph(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	inputReference, _ := access.Parse("memory:input")
	outputReference, _ := access.Parse("memory:output")
	input, _ := job.InputFromReference(inputReference)
	output, _ := job.OutputToReference(outputReference)
	request, err := job.New([]job.Input{input}, []job.Output{output}, requested, job.WithMappings(mappings...))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func mappingContexts(t testing.TB, format mediaformat.Format, node job.NodeID, ids ...stream.ID) graph.CompileContexts {
	t.Helper()
	selection, err := mediaformat.NewSelection(format, ids...)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := mediaformat.WithSelection(plugin.CompileContext{}, selection)
	if err != nil {
		t.Fatal(err)
	}
	return graph.NewCompileContexts(map[job.NodeID]plugin.CompileContext{node: prepared})
}

func evaluateMappings(t testing.TB, request job.Job, contexts graph.CompileContexts, components ...plugin.Component) ([]plan.Mapping, error) {
	t.Helper()
	index := solveIndex(t, components...)
	requested, _ := request.Graph()
	evaluation, err := graph.EvaluateBounded(index, requested, contexts, nil)
	if err != nil {
		return nil, err
	}
	compiled, ok := evaluation.Graph()
	if !ok {
		t.Fatal("mapping fixture graph did not compile")
	}
	planner := planner{
		index:    index,
		request:  request,
		contexts: contexts,
		formats: map[formatBoundary]job.NodeID{
			{direction: plan.InputBoundary, choice: 0}:  "reader",
			{direction: plan.OutputBoundary, choice: 0}: "writer",
		},
	}
	return planner.projectMappings(compiled)
}

func mappingDescriptor(id stream.ID) stream.Descriptor {
	return stream.MustDescriptor(id, solveSchemaA.Descriptor(), timing.MustBase(1, 48_000), property.New())
}

func hasDiagnostic(err error, code string) bool {
	for _, item := range diagnostic.ItemsOf(err) {
		if item.Code == code {
			return true
		}
	}
	return false
}
