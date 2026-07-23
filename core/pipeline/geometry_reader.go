package pipeline

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
