package job

import (
	"testing"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type jobSourceID struct{}
type jobSinkID struct{}

func TestChoicesAreExclusiveAndReferencesStayRedacted(t *testing.T) {
	reference, err := access.Parse("https://user:secret@example.com/in?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	input, err := InputFromReference(reference)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := input.Reference()
	if !ok {
		t.Fatal("reference input lost its reference")
	}
	if _, ok := input.Source(); ok {
		t.Fatal("reference input also exposed a source")
	}
	if got.String() == reference.Canonical() {
		t.Fatal("ordinary reference rendering exposed canonical secret")
	}
}

func TestGraphRejectsDuplicateIdentityAndMapping(t *testing.T) {
	source := NewNode("source", plugin.IdentityOf[jobSourceID](), config.NewPatch())
	sink := NewNode("sink", plugin.IdentityOf[jobSinkID](), config.NewPatch())
	mapping := MapStream(0, stream.ID("audio"), 0)
	_, err := NewGraph(
		[]Node{source, source, sink},
		[]Edge{Connect(At("source", "out"), At("sink", "in"))},
		mapping,
		mapping,
	)
	if err == nil {
		t.Fatal("invalid graph was accepted")
	}
	codes := make(map[string]bool)
	for _, item := range diagnostic.ItemsOf(err) {
		codes[item.Code] = true
	}
	if !codes["job.duplicate-node"] || !codes["job.duplicate-mapping"] {
		t.Fatalf("graph diagnostics = %v", err)
	}
}

func TestGraphCopiesCallerSlices(t *testing.T) {
	nodes := []Node{
		NewNode("source", plugin.IdentityOf[jobSourceID](), config.NewPatch()),
		NewNode("sink", plugin.IdentityOf[jobSinkID](), config.NewPatch()),
	}
	edges := []Edge{Connect(At("source", "out"), At("sink", "in"))}
	graph, err := NewGraph(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	nodes[0] = Node{}
	edges[0] = Edge{}
	if !graph.Nodes()[0].Valid() || !graph.Edges()[0].Valid() {
		t.Fatal("graph retained caller slice storage")
	}
}
