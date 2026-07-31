package pipeline

import (
	"fmt"

	"github.com/godexture/godec/core/node"
)

type preparationPlan struct {
	nodes    []node.Node
	preloads []node.StagedInput
	run      []node.Node
	runIndex []int
}

func planPreparation(definitions []NodeDef, edges []EdgeDef, nodes map[string]node.Node) (preparationPlan, error) {
	incoming := make(map[string][]EdgeDef)
	for _, edge := range edges {
		incoming[edge.ToNode] = append(incoming[edge.ToNode], edge)
	}
	preloadRoots := make(map[string]struct{})
	preloads := make([]node.StagedInput, 0)
	for _, definition := range definitions {
		staged, ok := definition.Node.(node.StagedInput)
		if !ok {
			continue
		}
		hasPreload := false
		for port, phase := range staged.InputPhases() {
			if phase != node.InputPhasePreload {
				continue
			}
			hasPreload = true
			connected := false
			for _, edge := range incoming[definition.ID] {
				if edge.ToPort == port {
					connected = true
					break
				}
			}
			if !connected {
				return preparationPlan{}, fmt.Errorf("%w: preload port %s:%s is not connected", ErrInvalidPipeline, definition.ID, port)
			}
		}
		if hasPreload {
			preloadRoots[definition.ID] = struct{}{}
			preloads = append(preloads, staged)
		}
	}

	prepareIDs := make(map[string]struct{})
	var visit func(string) error
	visit = func(id string) error {
		if _, root := preloadRoots[id]; root {
			return fmt.Errorf("%w: preload path cannot pass through consumer %s", ErrInvalidPipeline, id)
		}
		if _, exists := prepareIDs[id]; exists {
			return nil
		}
		if _, exists := nodes[id]; !exists {
			return fmt.Errorf("%w: preload node not found: %s", ErrInvalidPipeline, id)
		}
		prepareIDs[id] = struct{}{}
		for _, edge := range incoming[id] {
			if err := visit(edge.FromNode); err != nil {
				return err
			}
		}
		return nil
	}
	for _, edge := range edges {
		if _, root := preloadRoots[edge.ToNode]; root {
			staged := nodes[edge.ToNode].(node.StagedInput)
			if staged.InputPhases()[edge.ToPort] == node.InputPhasePreload {
				if err := visit(edge.FromNode); err != nil {
					return preparationPlan{}, err
				}
			}
		}
	}

	plan := preparationPlan{preloads: preloads}
	for i, definition := range definitions {
		current := definition.Node
		if _, prepare := prepareIDs[definition.ID]; prepare {
			plan.nodes = append(plan.nodes, current)
			continue
		}
		plan.run = append(plan.run, current)
		plan.runIndex = append(plan.runIndex, i)
	}
	return plan, nil
}
