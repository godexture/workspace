package pipeline

import (
	"fmt"
	"sync"

	"github.com/godexture/core/node"
)

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

type geometryState uint8

const (
	geometryOpen geometryState = iota
	geometryTransferred
	geometryClosing
	geometryClosed
)

// Geometry owns all added nodes until Builder.Build transfers them to a
// Pipeline. Close releases nodes when a negotiated geometry is abandoned.
type Geometry struct {
	mu       sync.Mutex
	state    geometryState
	nodes    []NodeDef
	nodeIDs  map[string]struct{}
	edges    []EdgeDef
	done     chan struct{}
	closeErr error
}

func NewGeometry() *Geometry {
	return &Geometry{
		nodeIDs: make(map[string]struct{}),
		done:    make(chan struct{}),
	}
}

func (g *Geometry) AddNode(id string, n node.Node) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != geometryOpen {
		return fmt.Errorf("%w: geometry does not accept nodes", ErrInvalidPipeline)
	}
	if id == "" {
		return fmt.Errorf("%w: node ID must not be empty", ErrInvalidPipeline)
	}
	if isNilNode(n) {
		return fmt.Errorf("%w: node %q is nil", ErrInvalidPipeline, id)
	}
	if _, exists := g.nodeIDs[id]; exists {
		return fmt.Errorf("%w: duplicate node ID %q", ErrInvalidPipeline, id)
	}
	g.nodeIDs[id] = struct{}{}
	g.nodes = append(g.nodes, NodeDef{ID: id, Node: n})
	return nil
}

func (g *Geometry) AddEdge(fromNode, fromPort, toNode, toPort string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != geometryOpen {
		return fmt.Errorf("%w: geometry does not accept edges", ErrInvalidPipeline)
	}
	if fromNode == "" || fromPort == "" || toNode == "" || toPort == "" {
		return fmt.Errorf("%w: edge endpoints and ports must not be empty", ErrInvalidPipeline)
	}
	g.edges = append(g.edges, EdgeDef{
		FromNode: fromNode,
		FromPort: fromPort,
		ToNode:   toNode,
		ToPort:   toPort,
	})
	return nil
}

func (g *Geometry) Nodes() []NodeDef {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]NodeDef(nil), g.nodes...)
}

func (g *Geometry) Edges() []EdgeDef {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]EdgeDef(nil), g.edges...)
}

func (g *Geometry) Close() error {
	g.mu.Lock()
	if g.state == geometryTransferred {
		g.mu.Unlock()
		return nil
	}
	if g.state == geometryClosing {
		done := g.done
		g.mu.Unlock()
		<-done
		g.mu.Lock()
		err := g.closeErr
		g.mu.Unlock()
		return err
	}
	if g.state == geometryClosed {
		err := g.closeErr
		g.mu.Unlock()
		return err
	}
	g.state = geometryClosing
	nodes := g.nodes
	g.nodes = nil
	g.edges = nil
	g.mu.Unlock()

	owned := make([]node.Node, len(nodes))
	for i := range nodes {
		owned[i] = nodes[i].Node
	}
	err := closeNodes(owned)

	g.mu.Lock()
	g.closeErr = err
	g.state = geometryClosed
	close(g.done)
	g.mu.Unlock()
	return err
}

func (g *Geometry) take() ([]NodeDef, []EdgeDef, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != geometryOpen {
		return nil, nil, fmt.Errorf("%w: geometry is not open", ErrInvalidPipeline)
	}
	g.state = geometryTransferred
	nodes := g.nodes
	edges := g.edges
	g.nodes = nil
	g.edges = nil
	g.nodeIDs = nil
	return nodes, edges, nil
}
