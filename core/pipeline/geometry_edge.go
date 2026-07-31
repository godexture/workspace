package pipeline

import (
	"fmt"
)

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
