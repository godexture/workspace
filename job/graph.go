package job

import (
	"strings"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

type NodeID string

func (id NodeID) Valid() bool    { return strings.TrimSpace(string(id)) != "" }
func (id NodeID) String() string { return string(id) }

// Node is one explicitly requested, pinned component instance.
type Node struct {
	id        NodeID
	component plugin.Identity
	config    config.Patch
}

func NewNode(id NodeID, component plugin.Identity, patch config.Patch) Node {
	return Node{id: NodeID(strings.TrimSpace(string(id))), component: component, config: patch}
}

func (n Node) Valid() bool                { return n.id.Valid() && !n.component.IsZero() }
func (n Node) ID() NodeID                 { return n.id }
func (n Node) Component() plugin.Identity { return n.component }
func (n Node) Config() config.Patch       { return n.config }

// Port identifies one port on one requested node.
type Port struct {
	node NodeID
	port string
}

func At(node NodeID, port string) Port {
	return Port{node: node, port: strings.TrimSpace(port)}
}

func (p Port) Valid() bool    { return p.node.Valid() && p.port != "" }
func (p Port) Node() NodeID   { return p.node }
func (p Port) ID() string     { return p.port }
func (p Port) String() string { return p.node.String() + ":" + p.port }

type Edge struct {
	from Port
	to   Port
}

func Connect(from, to Port) Edge { return Edge{from: from, to: to} }

func (e Edge) Valid() bool { return e.from.Valid() && e.to.Valid() }
func (e Edge) From() Port  { return e.from }
func (e Edge) To() Port    { return e.to }

// Mapping is an exact inspected stream mapping. Selector expressions remain
// deferred until M7's multi-stream format supplies their real consumer.
type Mapping struct {
	input  int
	stream stream.ID
	output int
}

func MapStream(input int, id stream.ID, output int) Mapping {
	return Mapping{input: input, stream: id, output: output}
}

func (m Mapping) Valid() bool       { return m.input >= 0 && !m.stream.IsZero() && m.output >= 0 }
func (m Mapping) Input() int        { return m.input }
func (m Mapping) Stream() stream.ID { return m.stream }
func (m Mapping) Output() int       { return m.output }

// Graph is an immutable requested graph. It validates only caller-owned
// identities here; component port semantics belong to internal/graph.
type Graph struct {
	nodes    []Node
	edges    []Edge
	mappings []Mapping
}

func NewGraph(nodes []Node, edges []Edge, mappings ...Mapping) (Graph, error) {
	var items []diagnostic.Item
	seenNodes := make(map[NodeID]struct{}, len(nodes))
	for _, node := range nodes {
		if !node.Valid() {
			items = append(items, diagnostic.NewItem("job.invalid-node", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.ID().String()}, "requested graph node is invalid", nil))
			continue
		}
		if _, exists := seenNodes[node.ID()]; exists {
			items = append(items, diagnostic.NewItem("job.duplicate-node", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.ID().String()}, "requested graph node ID is repeated", nil))
		}
		seenNodes[node.ID()] = struct{}{}
	}
	if len(nodes) == 0 {
		items = append(items, diagnostic.NewItem("job.empty-graph", diagnostic.ErrorSeverity, diagnostic.Path{}, "requested graph has no nodes", nil))
	}
	seenEdges := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		if !edge.Valid() {
			items = append(items, diagnostic.NewItem("job.invalid-edge", diagnostic.ErrorSeverity, diagnostic.Path{}, "requested graph edge is invalid", nil))
			continue
		}
		if _, exists := seenNodes[edge.From().Node()]; !exists {
			items = append(items, diagnostic.NewItem("job.unknown-node", diagnostic.ErrorSeverity, diagnostic.Path{Component: edge.From().Node().String()}, "edge source node is not in the requested graph", nil))
		}
		if _, exists := seenNodes[edge.To().Node()]; !exists {
			items = append(items, diagnostic.NewItem("job.unknown-node", diagnostic.ErrorSeverity, diagnostic.Path{Component: edge.To().Node().String()}, "edge destination node is not in the requested graph", nil))
		}
		key := edge.From().String() + "->" + edge.To().String()
		if _, exists := seenEdges[key]; exists {
			items = append(items, diagnostic.NewItem("job.duplicate-edge", diagnostic.ErrorSeverity, diagnostic.Path{Descriptor: key}, "requested graph edge is repeated", nil))
		}
		seenEdges[key] = struct{}{}
	}
	seenMappings := make(map[Mapping]struct{}, len(mappings))
	for _, mapping := range mappings {
		if !mapping.Valid() {
			items = append(items, diagnostic.NewItem("job.invalid-mapping", diagnostic.ErrorSeverity, diagnostic.Path{}, "stream mapping is invalid", nil))
			continue
		}
		if _, exists := seenMappings[mapping]; exists {
			items = append(items, diagnostic.NewItem("job.duplicate-mapping", diagnostic.ErrorSeverity, diagnostic.Path{}, "stream mapping is repeated", nil))
		}
		seenMappings[mapping] = struct{}{}
	}
	if hasErrors(items) {
		return Graph{}, diagnostic.NewError(items...)
	}
	return Graph{
		nodes:    append([]Node(nil), nodes...),
		edges:    append([]Edge(nil), edges...),
		mappings: append([]Mapping(nil), mappings...),
	}, nil
}

func (g Graph) Valid() bool         { return len(g.nodes) != 0 }
func (g Graph) Nodes() []Node       { return append([]Node(nil), g.nodes...) }
func (g Graph) Edges() []Edge       { return append([]Edge(nil), g.edges...) }
func (g Graph) Mappings() []Mapping { return append([]Mapping(nil), g.mappings...) }

func hasErrors(items []diagnostic.Item) bool {
	for _, item := range items {
		if item.Severity == diagnostic.ErrorSeverity {
			return true
		}
	}
	return false
}
