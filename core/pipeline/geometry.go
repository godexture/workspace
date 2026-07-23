package pipeline

import (
	"errors"
	"fmt"
	"sync"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
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

func (g *Geometry) AddNode(id string, n node.Node) error {
	return g.AddNodeDef(NodeDef{ID: id, Node: n})
}

func (g *Geometry) AddNodeDef(definition NodeDef) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != geometryOpen {
		return fmt.Errorf("%w: geometry does not accept nodes", ErrInvalidPipeline)
	}
	if definition.ID == "" {
		return fmt.Errorf("%w: node ID must not be empty", ErrInvalidPipeline)
	}
	if isNilNode(definition.Node) {
		return fmt.Errorf("%w: node %q is nil", ErrInvalidPipeline, definition.ID)
	}
	if _, exists := g.nodeIDs[definition.ID]; exists {
		return fmt.Errorf("%w: duplicate node ID %q", ErrInvalidPipeline, definition.ID)
	}
	definition.Description.ID = definition.ID
	definition.Description.Inputs = media.CloneStreams(definition.Description.Inputs)
	definition.Description.Outputs = media.CloneStreams(definition.Description.Outputs)
	g.nodeIDs[definition.ID] = struct{}{}
	g.nodes = append(g.nodes, definition)
	return nil
}

func (g *Geometry) SetNodeDescription(id string, description NodeDescription) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != geometryOpen {
		return fmt.Errorf("%w: geometry does not accept node descriptions", ErrInvalidPipeline)
	}
	for i := range g.nodes {
		if g.nodes[i].ID == id {
			description.ID = id
			description.Inputs = media.CloneStreams(description.Inputs)
			description.Outputs = media.CloneStreams(description.Outputs)
			g.nodes[i].Description = description
			return nil
		}
	}
	return fmt.Errorf("%w: node not found: %s", ErrInvalidPipeline, id)
}

// AddResourceCloser registers a shared resource (such as a worker pool held
// by several nodes) to be closed once every node in the eventual Pipeline has
// closed, or immediately if the geometry is abandoned instead of built.
func (g *Geometry) AddResourceCloser(closer func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != geometryOpen {
		return fmt.Errorf("%w: geometry does not accept resource closers", ErrInvalidPipeline)
	}
	if closer == nil {
		return fmt.Errorf("%w: resource closer must not be nil", ErrInvalidPipeline)
	}
	g.resourceClosers = append(g.resourceClosers, closer)
	return nil
}

func (g *Geometry) AddEdge(fromNode, fromPort, toNode, toPort string) error {
	return g.AddEdgeDef(EdgeDef{FromNode: fromNode, FromPort: fromPort, ToNode: toNode, ToPort: toPort})
}

func (g *Geometry) AddEdgeDef(definition EdgeDef) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != geometryOpen {
		return fmt.Errorf("%w: geometry does not accept edges", ErrInvalidPipeline)
	}
	if definition.FromNode == "" || definition.FromPort == "" || definition.ToNode == "" || definition.ToPort == "" {
		return fmt.Errorf("%w: edge endpoints and ports must not be empty", ErrInvalidPipeline)
	}
	definition.Stream = definition.Stream.Clone()
	g.edges = append(g.edges, definition)
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

func (g *Geometry) Description() Description {
	g.mu.Lock()
	defer g.mu.Unlock()
	return descriptionFromDefinitions(g.nodes, g.edges).Clone()
}

func descriptionFromDefinitions(nodes []NodeDef, edges []EdgeDef) Description {
	description := Description{
		Nodes: make([]NodeDescription, len(nodes)),
		Edges: append([]EdgeDescription(nil), edges...),
	}
	for i, definition := range nodes {
		description.Nodes[i] = definition.Description
		description.Nodes[i].ID = definition.ID
	}
	return description
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
	closers := g.resourceClosers
	g.nodes = nil
	g.edges = nil
	g.resourceClosers = nil
	g.mu.Unlock()

	owned := make([]node.Node, len(nodes))
	for i := range nodes {
		owned[i] = nodes[i].Node
	}
	err := closeNodes(owned)
	err = errors.Join(err, closeResources(closers))

	g.mu.Lock()
	g.closeErr = err
	g.state = geometryClosed
	close(g.done)
	g.mu.Unlock()
	return err
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

func closeResources(closers []func() error) error {
	var result error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i](); err != nil {
			result = errors.Join(result, fmt.Errorf("close resource %d: %w", i, err))
		}
	}
	return result
}
