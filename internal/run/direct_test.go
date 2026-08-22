package run

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
)

// directTemplate builds one producer feeding a direct serial fan-in and a sink.
// producers names the source nodes and how many routes each one carries.
func directTemplate(t testing.TB, kind drive.Kind, routes ...int) (Template, error) {
	t.Helper()
	typ := templateInput
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.SerialFanIn), flow.Direct())},
		[]flow.Port{flow.Out("out", typ)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	descriptor := stream.MustDescriptor("route", typ.Descriptor(), timing.Base{}, property.New())

	nodes := make([]Node, 0, len(routes)+2)
	edges := make([]job.Edge, 0, len(routes)+1)
	for index, count := range routes {
		id := job.NodeID("source-" + string(rune('a'+index)))
		many := []flow.PortOption{flow.Many()}
		binding := drive.NewRoutedSource("out", typ)
		if kind == drive.Source {
			many = nil
			binding = drive.NewSource("out", typ)
		}
		outputs := make([]flow.PortDescriptor[stream.Descriptor], count)
		for route := range outputs {
			outputs[route] = flow.Describe("out", descriptor)
		}
		nodes = append(nodes, Node{
			ID:        id,
			Shape:     flow.NewShape(nil, []flow.Port{flow.Out("out", typ, many...)}),
			Outputs:   flow.NewDescriptors(outputs...),
			Execution: binding,
		})
		edges = append(edges, job.Connect(job.At(id, "out"), job.At("join", "in")))
	}
	nodes = append(nodes,
		Node{ID: "join", Shape: joinShape, Execution: drive.NewJoiner("in", typ, flow.SerialFanIn, "out", typ)},
		Node{ID: "sink", Shape: sinkShape, Execution: drive.NewSink("in", typ)},
	)
	edges = append(edges, job.Connect(job.At("join", "out"), job.At("sink", "in")))
	return compileFixture(nodes, edges, templateQueue, job.AlignmentPolicy{})
}

// TestDirectManyInputAcceptsOneRoutedProducer is the positive case behind
// flow.Direct: the routes of one routed reader reach the port unqueued, so the
// order it emits in is the order the joiner sees.
func TestDirectManyInputAcceptsOneRoutedProducer(t *testing.T) {
	template, err := directTemplate(t, drive.RoutedSource, 2)
	if err != nil {
		t.Fatal(err)
	}
	runtime := template.Projection()
	if len(runtime.FanIns) != 1 || !runtime.FanIns[0].Direct || runtime.FanIns[0].Policy != flow.SerialFanIn {
		t.Fatalf("direct serial fan-in projection = %#v", runtime.FanIns)
	}
	for _, buffer := range runtime.Buffers {
		if buffer.ToNode == "join" {
			t.Fatalf("direct serial input projected a buffer: %#v", buffer)
		}
	}
	for _, connection := range template.connections {
		if template.nodes[connection.to].id == "join" && connection.reason != 0 {
			t.Fatalf("direct serial connection has a buffer reason: %#v", connection)
		}
	}
}

// TestDirectManyInputRejectsUnsatisfiableProducers covers what a component that
// lays its output out in arrival order cannot survive: several tasks feeding
// the port, and a producer that is not one typed routed emitter.
func TestDirectManyInputRejectsUnsatisfiableProducers(t *testing.T) {
	for name, routes := range map[string][]int{
		"two routed producers": {1, 1},
		"three producers":      {2, 1, 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := directTemplate(t, drive.RoutedSource, routes...); !errors.Is(err, ErrDirectIsland) {
				t.Fatalf("concurrent producers into a direct port = %v, want ErrDirectIsland", err)
			}
		})
	}
	if _, err := directTemplate(t, drive.Source, 1); !errors.Is(err, ErrDirectIsland) {
		t.Fatalf("plain source into a direct port = %v, want ErrDirectIsland", err)
	}
}

// TestDirectManyInputRunsInTheProducersEmitOrder is the behavioral half: a
// routed reader that finishes one route before starting the next must be
// observed that way, with no task between it and the joiner.
func TestDirectManyInputRunsInTheProducersEmitOrder(t *testing.T) {
	template, err := directTemplate(t, drive.RoutedSource, 2)
	if err != nil {
		t.Fatal(err)
	}
	typ := templateInput
	sourceShape := flow.NewShape(nil, []flow.Port{flow.Out("out", typ, flow.Many())})
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.SerialFanIn), flow.Direct())},
		[]flow.Port{flow.Out("out", typ)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)

	steps := make([][]routedTemplateEmission, 0, 48)
	for value := range 24 {
		steps = append(steps, []routedTemplateEmission{{route: 0, value: value}})
	}
	for value := 24; value < 48; value++ {
		steps = append(steps, []routedTemplateEmission{{route: 1, value: value}})
	}
	joiner := &serialOrdinalJoiner{templateOperator: templateOperator{shape: joinShape}}
	writer := &templateWriter{templateOperator: templateOperator{shape: sinkShape}}
	value := buildIsland(t, template,
		&routedTemplateReader{
			templateOperator: templateOperator{shape: sourceShape},
			steps:            steps,
			items:            make([]flow.Item[int], 2),
			failure:          io.EOF,
		},
		joiner,
		writer,
	)
	if sources, edgeTasks := value.execution.TaskCounts(); sources != 1 || edgeTasks != 1 {
		t.Fatalf("direct island task counts = sources %d edges %d, want the reader plus only the join output buffer", sources, edgeTasks)
	}
	value.run(context.Background())
	if !value.succeeded() {
		t.Fatalf("run = %#v, ledger = %#v", value.report, value.events())
	}
	got := joiner.Inputs()
	if len(got) != len(steps) {
		t.Fatalf("direct island delivered %d items, want %d", len(got), len(steps))
	}
	for index := 1; index < len(got); index++ {
		if got[index] < got[index-1] {
			t.Fatalf("direct island delivered route %d after route %d at %d: %v", got[index], got[index-1], index, got)
		}
	}
}
