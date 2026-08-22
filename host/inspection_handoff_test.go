package host

import (
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

type inspectHandoffPluginID struct{}
type inspectHandoffWriterID struct{}
type inspectHandoffOtherFormatID struct{}

func TestHandoffInspectionsSkipsDifferentFormatWriter(t *testing.T) {
	other, err := mediaformat.Define[inspectHandoffOtherFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	source, _, _, _ := boundaryComponentsWith(nil, nil, nil)
	writer := manyFormatSelectionComponent[inspectHandoffWriterID](other)
	host := newInspectionHandoffHost(t, writer, source)
	requested, err := job.NewGraph(
		[]job.Node{
			job.NewNode("source", source.Identity(), config.NewPatch()),
			job.NewNode("writer", writer.Identity(), config.NewPatch()),
		},
		[]job.Edge{job.Connect(job.At("source", "out"), job.At("writer", "in"))},
	)
	if err != nil {
		t.Fatal(err)
	}
	contexts := map[job.NodeID]plugin.CompileContext{"writer": {}}
	inspected := []inspectedFormat{{source: "source", boundary: "input", value: mediaformat.NewInspection(boundaryFormat(), 44)}}
	if err := host.handoffInspections(requested, contexts, inspected); err != nil {
		t.Fatal(err)
	}
	if _, ok := mediaformat.InspectionOf[int](contexts["writer"], other); ok {
		t.Fatal("different-format writer received an inspection")
	}
	if _, ok := mediaformat.InspectionOf[int](contexts["writer"], boundaryFormat()); ok {
		t.Fatal("writer received an inspection from a different format")
	}
}

func TestHandoffInspectionsDiagnosesMultipleSourcesDeterministically(t *testing.T) {
	source, _, _, _ := boundaryComponentsWith(nil, nil, nil)
	writer := manyFormatSelectionComponent[inspectHandoffWriterID](boundaryFormat())
	host := newInspectionHandoffHost(t, writer, source)
	requested, err := job.NewGraph(
		[]job.Node{
			job.NewNode("z-source", source.Identity(), config.NewPatch()),
			job.NewNode("a-source", source.Identity(), config.NewPatch()),
			job.NewNode("writer", writer.Identity(), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("z-source", "out"), job.At("writer", "in")),
			job.Connect(job.At("a-source", "out"), job.At("writer", "in")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	contexts := map[job.NodeID]plugin.CompileContext{"writer": {}}
	inspection := mediaformat.NewInspection(boundaryFormat(), 44)
	inspected := []inspectedFormat{
		{source: "z-source", boundary: "z-input", value: inspection},
		{source: "a-source", boundary: "a-input", value: inspection},
	}
	err = host.handoffInspections(requested, contexts, inspected)
	if err == nil {
		t.Fatal("multiple inspected sources were accepted for one writer")
	}
	items := Diagnostics(err)
	if len(items) != 1 || items[0].Code != "prepare.inspect-handoff" {
		t.Fatalf("handoff diagnostics = %#v", items)
	}
	detail := items[0].Detail
	if detail["sourceCount"] != "2" || detail["sources"] != "a-source,z-source" || detail["writeNode"] != "writer" {
		t.Fatalf("handoff detail = %#v", detail)
	}
}

func TestHandoffInspectionsKeepsIndependentFormatBranchesSeparate(t *testing.T) {
	source, _, _, _ := boundaryComponentsWith(nil, nil, nil)
	writer := manyFormatSelectionComponent[inspectHandoffWriterID](boundaryFormat())
	host := newInspectionHandoffHost(t, writer, source)
	requested, err := job.NewGraph(
		[]job.Node{
			job.NewNode("source-a", source.Identity(), config.NewPatch()),
			job.NewNode("writer-a", writer.Identity(), config.NewPatch()),
			job.NewNode("source-b", source.Identity(), config.NewPatch()),
			job.NewNode("writer-b", writer.Identity(), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("source-a", "out"), job.At("writer-a", "in")),
			job.Connect(job.At("source-b", "out"), job.At("writer-b", "in")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	contexts := map[job.NodeID]plugin.CompileContext{"writer-a": {}, "writer-b": {}}
	inspected := []inspectedFormat{
		{source: "source-a", boundary: "input-a", value: mediaformat.NewInspection(boundaryFormat(), 11)},
		{source: "source-b", boundary: "input-b", value: mediaformat.NewInspection(boundaryFormat(), 22)},
	}
	if err := host.handoffInspections(requested, contexts, inspected); err != nil {
		t.Fatal(err)
	}
	if value, ok := mediaformat.InspectionOf[int](contexts["writer-a"], boundaryFormat()); !ok || value != 11 {
		t.Fatalf("writer-a inspection = %d/%v", value, ok)
	}
	if value, ok := mediaformat.InspectionOf[int](contexts["writer-b"], boundaryFormat()); !ok || value != 22 {
		t.Fatalf("writer-b inspection = %d/%v", value, ok)
	}
}

func newInspectionHandoffHost(t *testing.T, writer plugin.Component, extras ...plugin.Component) *Host {
	t.Helper()
	components := append([]plugin.Component{writer}, extras...)
	set := plugin.NewSet(plugin.Define[inspectHandoffPluginID](plugin.Descriptor{DisplayName: "inspection handoff", Version: "1"}, components...))
	host, err := New(Plugins(set))
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func manyFormatSelectionComponent[Marker any](value mediaformat.Format) plugin.Component {
	shape := flow.NewShape(
		[]flow.Port{flow.In("in", access.Bytes(), flow.Many(), flow.WithFanIn(flow.SerialFanIn))},
		[]flow.Port{flow.Out("writes", access.Writes())},
	)
	spec := plugin.Spec[formatSelectionConfig, flow.Shape, int]{
		Ports: shape,
		Compile: func(plugin.CompileContext, formatSelectionConfig, flow.Descriptors[int]) (plugin.Compiled[flow.Shape, int], error) {
			return plugin.Compiled[flow.Shape, int]{Plan: shape, Outputs: flow.NewDescriptors(flow.Describe("writes", 1))}, nil
		},
		Open: func(plugin.OpenContext, flow.Shape) (flow.Operator, error) {
			return formatSelectionOperator{shape: shape}, nil
		},
	}
	configuration := config.Struct[formatSelectionConfigID](func() formatSelectionConfig { return formatSelectionConfig{} }).Version("1").Build()
	return plugin.NewComponent[Marker](plugin.Descriptor{DisplayName: "Format writer"}, configuration,
		plugin.WithSpec(spec),
		mediaformat.Write(value, access.NewRequirements(access.AllOf(access.SequentialWrite))),
	)
}
