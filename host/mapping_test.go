package host

import (
	"testing"

	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type mappingContextFormatID struct{}

func TestMappingSelectionPreservesPreparedInspection(t *testing.T) {
	format, err := mediaformat.Define[mappingContextFormatID](nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := mediaformat.WithInspection(plugin.CompileContext{}, mediaformat.NewInspection(format, "inspection"))
	if err != nil {
		t.Fatal(err)
	}
	contexts := graph.NewCompileContexts(map[job.NodeID]plugin.CompileContext{"reader": prepared})
	selection, err := mediaformat.NewSelection(format, stream.ID("audio"))
	if err != nil {
		t.Fatal(err)
	}
	contexts, err = withMappingSelection(contexts, "reader", selection)
	if err != nil {
		t.Fatal(err)
	}
	ctx := contexts.For("reader")
	inspection, ok := mediaformat.InspectionOf[string](ctx, format)
	if !ok || inspection != "inspection" {
		t.Fatalf("prepared inspection = %q, %v", inspection, ok)
	}
	selected, ok := mediaformat.SelectionOf(ctx, format)
	if !ok || len(selected.Streams()) != 1 || selected.Streams()[0] != stream.ID("audio") {
		t.Fatalf("prepared selection = %#v, %v", selected.Streams(), ok)
	}
}
