package routing

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/godexture/godec/core/domain/manifest"
	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
	"github.com/godexture/godec/core/pipeline"
	"github.com/godexture/godec/core/registry"
)

// resolveEdge negotiates one edge from a resolved source into a specific
// destination port, splicing in bridge nodes when the source stream does not
// already satisfy requirements. It generalizes what satisfy used to do only
// for the linear main chain to every edge in the graph.
func (n *Negotiator) resolveEdge(
	source resolvedSource,
	requirements []manifest.Capability,
	toNode, toPort string,
	sourceStream media.StreamInfo,
	bridgeID *int,
	ownedNodes *[]node.Node,
	allPlans *[]transformPlan,
	graphEdges *[]pipeline.EdgeDef,
) (media.StreamInfo, error) {
	final, bridgePlans, err := n.satisfy(sourceStream, requirements, bridgeID)
	if err != nil {
		return media.StreamInfo{}, err
	}
	*ownedNodes = appendPlanNodes(*ownedNodes, bridgePlans)
	*allPlans = append(*allPlans, bridgePlans...)
	from, fromPort := source.nodeID, source.port
	for _, bridgePlan := range bridgePlans {
		*graphEdges = append(*graphEdges, pipeline.EdgeDef{FromNode: from, FromPort: fromPort, ToNode: bridgePlan.id, ToPort: "in", Stream: bridgePlan.inputs["in"]})
		from, fromPort = bridgePlan.id, "out"
	}
	*graphEdges = append(*graphEdges, pipeline.EdgeDef{FromNode: from, FromPort: fromPort, ToNode: toNode, ToPort: toPort, Stream: final})
	return final, nil
}

func (n *Negotiator) satisfy(
	current media.StreamInfo,
	required []manifest.Capability,
	bridgeID *int,
) (media.StreamInfo, []transformPlan, error) {
	if manifest.MatchesAny(required, current) {
		return current, nil, nil
	}
	if n.bridgeResolver == nil {
		return current, nil, manifest.Diagnose(current, required)
	}
	steps, err := n.bridgeResolver.ResolveBridge(current, required)
	if err != nil {
		return current, nil, err
	}
	expected := current
	for i, step := range steps {
		if !reflect.DeepEqual(step.Input, expected) {
			return current, nil, fmt.Errorf("bridge step %d input does not match the preceding stream", i)
		}
		expected = step.Output
	}
	if !manifest.MatchesAny(required, expected) {
		return current, nil, fmt.Errorf("bridge resolver returned a plan that does not satisfy the required capability")
	}

	plans := make([]transformPlan, 0, len(steps))
	for _, step := range steps {
		step := step
		id := fmt.Sprintf("bridge:%d", *bridgeID)
		*bridgeID++
		created, outputs, factoryErr := step.Manifest.Factory(media.StreamSet{"in": step.Input}, registry.TransformFactoryOptions{Config: step.Config})
		if factoryErr != nil {
			return current, nil, factoryErr
		}
		if outputErr := step.Manifest.ValidateOutputs(outputs); outputErr != nil {
			return current, nil, errors.Join(outputErr, created.Close())
		}
		output := outputs["out"]
		if !reflect.DeepEqual(output, step.Output) {
			return current, nil, errors.Join(fmt.Errorf("bridge factory output differs from the resolved bridge step"), created.Close())
		}
		plans = append(plans, transformPlan{
			id:           id,
			role:         manifest.RoleFilter,
			plugin:       step.Manifest.Name,
			config:       step.Config,
			resources:    step.Manifest.Resources,
			inputs:       media.StreamSet{"in": step.Input},
			outputs:      media.StreamSet{"out": step.Output},
			autoInserted: true,
			node:         created,
		})
	}
	return expected, plans, nil
}
