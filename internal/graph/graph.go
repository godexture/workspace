// Package graph compiles and validates pinned requested graphs. It is private
// so layout, indexing, and later runtime optimization can change without
// becoming a plugin contract.
package graph

import (
	"sort"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type Node struct {
	id          job.NodeID
	component   plugin.Component
	config      config.ResolvedView
	shape       flow.Shape
	inputs      flow.Descriptors[stream.Descriptor]
	compilation plugin.Compilation
}

func (n Node) ID() job.NodeID                              { return n.id }
func (n Node) Component() plugin.Identity                  { return n.component.Identity() }
func (n Node) Config() config.ResolvedView                 { return n.config }
func (n Node) Shape() flow.Shape                           { return n.shape.Clone() }
func (n Node) Inputs() flow.Descriptors[stream.Descriptor] { return copyDescriptors(n.inputs) }
func (n Node) Compilation() plugin.Compilation             { return n.compilation }
func (n Node) Outputs() flow.Descriptors[stream.Descriptor] {
	outputs, _ := plugin.OutputsOf[stream.Descriptor](n.compilation)
	return copyDescriptors(outputs)
}

type Graph struct {
	nodes []Node
	edges []job.Edge
	byID  map[job.NodeID]int
}

func (g Graph) Valid() bool { return len(g.nodes) != 0 && len(g.byID) == len(g.nodes) }

func (g Graph) Nodes() []Node {
	result := make([]Node, len(g.nodes))
	copy(result, g.nodes)
	return result
}

func (g Graph) Edges() []job.Edge { return append([]job.Edge(nil), g.edges...) }

func (g Graph) Lookup(id job.NodeID) (Node, bool) {
	index, ok := g.byID[id]
	if !ok {
		return Node{}, false
	}
	return g.nodes[index], true
}

// Open is an internal bridge used by the M4 explicit skeleton. M4-3 replaces
// direct graph access with a private Program, while preserving the same
// Compilation-to-Open binding.
func (g Graph) Open(ctx plugin.OpenContext, id job.NodeID) (flow.Operator, error) {
	node, ok := g.Lookup(id)
	if !ok {
		return nil, errUnknownNode(id)
	}
	return node.component.Open(ctx, node.compilation)
}

func newGraph(nodes []Node, edges []job.Edge) Graph {
	byID := make(map[job.NodeID]int, len(nodes))
	for index, node := range nodes {
		byID[node.id] = index
	}
	return Graph{nodes: nodes, edges: append([]job.Edge(nil), edges...), byID: byID}
}

func copyDescriptors(value flow.Descriptors[stream.Descriptor]) flow.Descriptors[stream.Descriptor] {
	return flow.NewDescriptors(value.Bindings()...)
}

func sortRequested(nodes []job.Node, edges []job.Edge) {
	sort.Slice(nodes, func(left, right int) bool { return nodes[left].ID().String() < nodes[right].ID().String() })
	sort.Slice(edges, func(left, right int) bool {
		leftKey := edges[left].From().String() + "->" + edges[left].To().String()
		rightKey := edges[right].From().String() + "->" + edges[right].To().String()
		return leftKey < rightKey
	})
}
