package solve

import (
	"errors"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plugin"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/program"
	"github.com/godexture/godec/internal/scratch"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/resource"
)

func (p *planner) buildProgram(compiled graph.Graph) (program.Program, error) {
	mappings, err := p.projectMappings(compiled)
	if err != nil {
		return program.Program{}, err
	}
	description := plan.Description{
		RequestedPolicy:    p.request.Policy(),
		EffectivePolicy:    p.policy,
		Budget:             p.budget,
		Usage:              p.usage,
		CatalogFingerprint: catalogFingerprint(p.index),
		Platform:           p.platform,
		Boundaries:         p.bound.Projections(),
		Mappings:           mappings,
		Warnings:           append([]string(nil), p.warnings...),
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
		if metadata.origin == plan.Automatic {
			if err := validateAutomaticCompilation(component, compilation, p.policy, p.platform); err != nil {
				return program.Program{}, p.planningError(err, nil, nil)
			}
		}
		if p.policy.Resources.Limited && !p.policy.Resources.Limit.Satisfies(compilation.Resources()) {
			return program.Program{}, solveDiagnostic("solve.unsupported", nil, p.usage, p.budget, "resource", nil)
		}
		description.Nodes = append(description.Nodes, plan.Node{
			ID:          node.ID().String(),
			Origin:      metadata.origin,
			Component:   component.Identity().String(),
			DisplayName: component.Descriptor().DisplayName,
			Variant:     component.Identity().String() + "#default",
			Version:     component.Descriptor().Version,
			Config:      summary,
			Inputs:      inputs,
			Outputs:     outputs,
			Reason:      metadata.reason,
			Effects:     compilation.Effects(),
			Contract:    component.Contract(),
			Resources:   compilation.Resources(),
			Scratch:     compilation.Scratch(),
			Temporary:   compilation.Temporary(),
			Estimate:    compilation.Estimate(),
		})
	}
	if err := strictMetadata(p.policy, description.Nodes); err != nil {
		return program.Program{}, err
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
	claims := make([]resource.Bytes, 0, len(description.Nodes)+len(description.Boundaries))
	for _, node := range description.Nodes {
		claims = append(claims, node.Scratch)
	}
	for _, boundary := range description.Boundaries {
		if boundary.Spool.Valid() {
			claims = append(claims, resource.Bytes(boundary.Spool.MaximumBytes()))
		}
	}
	reserved, err := scratch.Reserve(p.policy.Resources.ScratchMaxBytes, claims...)
	if err != nil {
		return program.Program{}, solveDiagnostic("solve.unsupported", nil, p.usage, p.budget, "scratch", nil)
	}
	var claimed resource.Bytes
	for _, node := range description.Nodes {
		claimed += node.Temporary
	}
	description.Scratch = plan.Scratch{
		Limit:              reserved.Limit(),
		Reserved:           reserved.Reserved(),
		TemporaryLimit:     p.policy.Resources.TemporaryMaxBytes,
		TemporaryClaimed:   claimed,
		TemporaryUnlimited: p.policy.Resources.TemporaryUnlimited,
	}
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

// strictMetadata refuses a plan that would lose metadata when the job said it
// must not. The encodings answer the same way either way -- what a carrier can
// hold is a fact about the carrier -- so the choice of whether that is fatal
// belongs here, once, rather than in each of them.
func strictMetadata(policy job.Policy, nodes []plan.Node) error {
	if policy.Metadata != job.StrictMetadata {
		return nil
	}
	var items []diagnostic.Item
	for _, node := range nodes {
		for _, effect := range node.Effects {
			if effect.Kind != plugin.MetadataEffect || effect.Loss == plugin.NoLoss {
				continue
			}
			detail := map[string]string{"node": node.ID, "reason": effect.Detail}
			if effect.Item != "" {
				detail["key"] = effect.Item
			}
			items = append(items, diagnostic.NewItem("solve.metadata-loss", diagnostic.ErrorSeverity,
				diagnostic.Path{Component: node.Component},
				"this conversion cannot carry metadata the job asked it to keep", detail))
		}
	}
	if len(items) == 0 {
		return nil
	}
	return diagnostic.NewError(items...)
}
