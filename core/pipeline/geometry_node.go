package pipeline

import (
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
)

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
