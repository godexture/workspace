package pipeline

import "github.com/godexture/core/node"

type NodeDef struct {
	ID   string
	Node node.Node
}

type EdgeDef struct {
	FromNode string
	FromPort string
	ToNode   string
	ToPort   string
}

type Geometry struct {
	Nodes []NodeDef
	Edges []EdgeDef
}

func NewGeometry() *Geometry {
	return &Geometry{}
}

func (g *Geometry) AddNode(id string, n node.Node) {
	g.Nodes = append(g.Nodes, NodeDef{ID: id, Node: n})
}

func (g *Geometry) AddEdge(fromNode, fromPort, toNode, toPort string) {
	g.Edges = append(g.Edges, EdgeDef{
		FromNode: fromNode,
		FromPort: fromPort,
		ToNode:   toNode,
		ToPort:   toPort,
	})
}
