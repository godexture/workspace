package solve

import (
	"errors"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/program"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
)

func (p *planner) buildProgram(compiled graph.Graph) (program.Program, error) {
	description := plan.Description{
		RequestedPolicy:    p.request.Policy(),
		EffectivePolicy:    p.policy,
		Budget:             p.budget,
		Usage:              p.usage,
		CatalogFingerprint: catalogFingerprint(p.index),
		Platform:           p.platform,
		Boundaries:         p.bound.Projections(),
	}
	for _, node := range compiled.Nodes() {
		component, ok := p.index.Lookup(node.Component())
		if !ok {
			return program.Program{}, errors.New("compiled component disappeared from catalog")
		}
		metadata, ok := p.nodes[node.ID()]
		if !ok {
			return program.Program{}, errors.New("compiled node has no planning origin")
		}
		summary := node.Config().Summary()
		if metadata.summary.Valid() {
			summary = metadata.summary
		}
		inputs, err := projectBindings(node.Inputs())
		if err != nil {
			return program.Program{}, err
		}
		outputs, err := projectBindings(node.Outputs())
		if err != nil {
			return program.Program{}, err
		}
		compilation := node.Compilation()
		if p.policy.Resources.Limited && !p.policy.Resources.Limit.Satisfies(compilation.Resources()) {
			return program.Program{}, solveDiagnostic("solve.unsupported", nil, p.usage, p.budget, "resource", nil)
		}
		description.Nodes = append(description.Nodes, plan.Node{
			ID:           node.ID().String(),
			Origin:       metadata.origin,
			Component:    component.Identity().String(),
			DisplayName:  component.Descriptor().DisplayName,
			Variant:      component.Identity().String() + "#default",
			Version:      component.Descriptor().Version,
			Config:       summary,
			Inputs:       inputs,
			Outputs:      outputs,
			Reason:       metadata.reason,
			Effects:      compilation.Effects(),
			Contract:     component.Contract(),
			Resources:    compilation.Resources(),
			Estimate:     compilation.Estimate(),
			Finalization: compilation.Finalization(),
		})
	}
	for _, edge := range compiled.Edges() {
		metadata, ok := p.edges[edgeKey(edge)]
		if !ok {
			return program.Program{}, errors.New("compiled edge has no planning origin")
		}
		description.Edges = append(description.Edges, plan.Edge{
			FromNode: edge.From().Node().String(),
			FromPort: edge.From().ID(),
			ToNode:   edge.To().Node().String(),
			ToPort:   edge.To().ID(),
			Origin:   metadata.origin,
			Reason:   metadata.reason,
		})
	}
	runtime, err := program.Project(compiled, p.policy)
	if err != nil {
		return program.Program{}, err
	}
	description.Runtime = runtime
	public, err := plan.New(description)
	if err != nil {
		return program.Program{}, err
	}
	return program.New(compiled, public, p.bound)
}

func projectBindings(bindings flow.Descriptors[stream.Descriptor]) ([]plan.PortDescriptor, error) {
	values := bindings.Bindings()
	result := make([]plan.PortDescriptor, len(values))
	for index, binding := range values {
		descriptor, err := plan.ProjectDescriptor(binding.Descriptor())
		if err != nil {
			return nil, err
		}
		result[index] = plan.PortDescriptor{Port: binding.Port(), Descriptor: descriptor}
	}
	return result, nil
}
