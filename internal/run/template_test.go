package run

import (
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plan"
)

type (
	templateInputID  struct{}
	templateMiddleID struct{}
	templateOutputID struct{}
)

var (
	templateInput  = schema.Define[templateInputID](schema.Traits[int]{})
	templateMiddle = schema.Define[templateMiddleID](schema.Traits[int]{})
	templateOutput = schema.Define[templateOutputID](schema.Traits[int]{})
)

func TestCompileFusesMaximalLinearProcessorIsland(t *testing.T) {
	nodes := []Node{
		{ID: "source", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)}), Execution: drive.NewSource("out", templateInput)},
		{ID: "first", Shape: flow.NewShape([]flow.Port{flow.In("in", templateInput)}, []flow.Port{flow.Out("out", templateMiddle)}), Execution: drive.NewProcessor("in", templateInput, "out", templateMiddle)},
		{ID: "second", Shape: flow.NewShape([]flow.Port{flow.In("in", templateMiddle)}, []flow.Port{flow.Out("out", templateOutput)}), Execution: drive.NewProcessor("in", templateMiddle, "out", templateOutput)},
		{ID: "sink", Shape: flow.NewShape([]flow.Port{flow.In("in", templateOutput)}, nil), Execution: drive.NewSink("in", templateOutput)},
	}
	edges := []job.Edge{
		job.Connect(job.At("source", "out"), job.At("first", "in")),
		job.Connect(job.At("first", "out"), job.At("second", "in")),
		job.Connect(job.At("second", "out"), job.At("sink", "in")),
	}
	template, err := Compile(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	runtime := template.Projection()
	if !template.Executable() || len(runtime.Islands) != 3 || len(runtime.Islands[1].Nodes) != 2 || runtime.Islands[1].Nodes[0] != "first" || runtime.Islands[1].Nodes[1] != "second" {
		t.Fatalf("islands = %#v", runtime.Islands)
	}
	if len(runtime.Buffers) != 2 || !runtime.Buffers[0].Reason.Has(plan.SourceBuffer) || !runtime.Buffers[1].Reason.Has(plan.SinkBuffer) {
		t.Fatalf("buffers = %#v", runtime.Buffers)
	}
}

func TestCompileProjectsFanoutAndCanonicalZip(t *testing.T) {
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", templateInput, flow.Many(), flow.WithFanIn(flow.ZipFanIn))},
		[]flow.Port{flow.Out("out", templateOutput)},
	)
	nodes := []Node{
		{ID: "a", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)}), Execution: drive.NewSource("out", templateInput)},
		{ID: "b", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)}), Execution: drive.NewSource("out", templateInput)},
		{ID: "join", Shape: joinShape, Execution: drive.NewJoiner("in", templateInput, flow.ZipFanIn, "out", templateOutput)},
		{ID: "sink", Shape: flow.NewShape([]flow.Port{flow.In("in", templateOutput)}, nil), Execution: drive.NewSink("in", templateOutput)},
	}
	edges := []job.Edge{
		job.Connect(job.At("b", "out"), job.At("join", "in")),
		job.Connect(job.At("a", "out"), job.At("join", "in")),
		job.Connect(job.At("join", "out"), job.At("sink", "in")),
	}
	template, err := Compile(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	runtime := template.Projection()
	if len(runtime.FanIns) != 1 || runtime.FanIns[0].Policy != flow.ZipFanIn || len(runtime.FanIns[0].Connections) != 2 || runtime.FanIns[0].Connections[0].FromNode != "a" || runtime.FanIns[0].Connections[1].FromNode != "b" {
		t.Fatalf("fan-in = %#v", runtime.FanIns)
	}
	if len(runtime.Buffers) != 3 || !runtime.Buffers[0].Reason.Has(plan.FanInBuffer) || !runtime.Buffers[1].Reason.Has(plan.FanInBuffer) {
		t.Fatalf("fan-in buffers = %#v", runtime.Buffers)
	}
}

func TestCompileKeepsPlanningOnlyGraphNonExecutable(t *testing.T) {
	template, err := Compile(
		[]Node{
			{ID: "source", Shape: flow.NewShape(nil, []flow.Port{flow.Out("out", templateInput)})},
			{ID: "sink", Shape: flow.NewShape([]flow.Port{flow.In("in", templateInput)}, nil)},
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("sink", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	if template.Executable() || template.Projection().Executable {
		t.Fatalf("planning-only template = %#v", template.Projection())
	}
}
