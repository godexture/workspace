package bind

import (
	"strconv"
	"strings"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (r Registry) selectCapabilities(nodes []job.Node, edges []job.Edge, entries []bound.Entry) ([]bound.Entry, error) {
	byNode := make(map[job.NodeID]job.Node, len(nodes))
	for _, node := range nodes {
		byNode[node.ID()] = node
	}
	result := make([]bound.Entry, len(entries))
	for index, entry := range entries {
		projection := entry.Projection()
		if projection.Kind != plan.ProviderBoundary {
			result[index] = entry
			continue
		}
		adjacent, err := adjacentBoundaryNode(projection, edges)
		if err != nil {
			return nil, err
		}
		node, ok := byNode[adjacent]
		if !ok {
			return nil, diagnostic.NewError(bindItem("bind.boundary-adjacent", plugin.Identity{}, "Access boundary has no adjacent component", map[string]string{"node": projection.Node}))
		}
		component, ok := r.index.Lookup(node.Component())
		if !ok {
			return nil, diagnostic.NewError(bindItem("bind.boundary-adjacent", node.Component(), "Access boundary adjacent component is not in the Host catalog", map[string]string{"node": adjacent.String()}))
		}
		var requirements access.Requirements
		var formatIdentity plugin.Identity
		if projection.Direction == plan.InputBoundary {
			trait, present := mediaformat.ReadOf(component)
			if !present || !trait.Valid() {
				return nil, missingFormatRequirements(projection, node)
			}
			requirements = trait.Requirements()
			formatIdentity = trait.Format().Identity()
		} else {
			trait, present := mediaformat.WriteOf(component)
			if !present || !trait.Valid() {
				return nil, missingFormatRequirements(projection, node)
			}
			requirements = trait.Requirements()
			formatIdentity = trait.Format().Identity()
		}
		available, err := access.NewCapabilities(projection.Available...)
		if err != nil {
			return nil, err
		}
		selection, ok := access.Select(available, requirements)
		if !ok {
			return nil, unsatisfiedCapabilities(projection, node, formatIdentity, requirements)
		}
		projection.Selected = selection.Capabilities()
		if projection.Direction == plan.InputBoundary {
			result[index] = bound.Source(projection, entry.Reference(), entry.SourceTrait())
		} else {
			result[index] = bound.Sink(projection, entry.Reference(), entry.SinkTrait())
		}
	}
	return result, nil
}

func adjacentBoundaryNode(boundary plan.Boundary, edges []job.Edge) (job.NodeID, error) {
	var adjacent []job.NodeID
	for _, edge := range edges {
		if boundary.Direction == plan.InputBoundary && edge.From().Node().String() == boundary.Node && edge.From().ID() == boundary.Port {
			adjacent = append(adjacent, edge.To().Node())
		}
		if boundary.Direction == plan.OutputBoundary && edge.To().Node().String() == boundary.Node && edge.To().ID() == boundary.Port {
			adjacent = append(adjacent, edge.From().Node())
		}
	}
	if len(adjacent) != 1 {
		return "", diagnostic.NewError(bindItem("bind.boundary-adjacent", plugin.Identity{}, "Access boundary must connect directly to one Format component", map[string]string{
			"node": boundary.Node, "connections": strconv.Itoa(len(adjacent)), "direction": boundaryDirection(boundary.Direction),
		}))
	}
	return adjacent[0], nil
}

func missingFormatRequirements(boundary plan.Boundary, adjacent job.Node) error {
	return diagnostic.NewError(bindItem("bind.format-requirement", adjacent.Component(), "Access boundary adjacent component has no capability requirements for its direction", map[string]string{
		"node": adjacent.ID().String(), "scheme": boundary.Scheme, "direction": boundaryDirection(boundary.Direction),
	}))
}

func unsatisfiedCapabilities(boundary plan.Boundary, adjacent job.Node, formatIdentity plugin.Identity, requirements access.Requirements) error {
	return diagnostic.NewError(bindItem("bind.capability-unsatisfied", adjacent.Component(), "Access Provider does not satisfy any Format capability alternative", map[string]string{
		"node":         adjacent.ID().String(),
		"scheme":       boundary.Scheme,
		"direction":    boundaryDirection(boundary.Direction),
		"format":       formatIdentity.String(),
		"available":    capabilityList(boundary.Available),
		"alternatives": alternativeList(requirements),
	}))
}

func boundaryDirection(direction plan.BoundaryDirection) string {
	if direction == plan.InputBoundary {
		return "read"
	}
	return "write"
}

func capabilityList(values []access.Capability) string {
	names := make([]string, len(values))
	for index, value := range values {
		names[index] = string(value)
	}
	return strings.Join(names, ",")
}

func alternativeList(requirements access.Requirements) string {
	values := make([]string, len(requirements.Alternatives))
	for index, alternative := range requirements.Alternatives {
		values[index] = capabilityList(alternative.Capabilities)
	}
	return strings.Join(values, "|")
}
