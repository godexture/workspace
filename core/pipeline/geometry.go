package pipeline

import (
	"fmt"
	"sync"

	"github.com/godexture/godec/core/node"
)

type NodeDef struct {
	ID          string
	Node        node.Node
	Description NodeDescription
}

type EdgeDef = EdgeDescription

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
	mu              sync.Mutex
	state           geometryState
	nodes           []NodeDef
	nodeIDs         map[string]struct{}
	edges           []EdgeDef
	resourceClosers []func() error
	done            chan struct{}
	closeErr        error
}

func NewGeometry() *Geometry {
	return &Geometry{
		nodeIDs: make(map[string]struct{}),
		done:    make(chan struct{}),
	}
}

func (g *Geometry) take() ([]NodeDef, []EdgeDef, []func() error, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != geometryOpen {
		return nil, nil, nil, fmt.Errorf("%w: geometry is not open", ErrInvalidPipeline)
	}
	g.state = geometryTransferred
	nodes := g.nodes
	edges := g.edges
	closers := g.resourceClosers
	g.nodes = nil
	g.edges = nil
	g.nodeIDs = nil
	g.resourceClosers = nil
	return nodes, edges, closers, nil
}
