package bind

import (
	"strconv"
	"strings"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (r Registry) selectCapabilities(nodes []job.Node, edges []job.Edge, entries []bound.Entry, policy job.ResourcePolicy) ([]bound.Entry, error) {
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
		adjacent, err := FormatNode(entry, edges)
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
			if !present {
				available, availableErr := access.NewCapabilities(projection.Available...)
				if availableErr != nil {
					return nil, availableErr
				}
				selection, selected := probeSelection(available)
				if !selected {
					return nil, probeCapabilityUnavailable(projection)
				}
				projection.Selected = selection.Capabilities()
				result[index] = bound.AutomaticSource(projection, entry.Reference(), entry.SourceTrait())
				continue
			}
			if !trait.Valid() {
				return nil, missingFormatRequirements(projection, node)
			}
			requirements = trait.Requirements()
			formatIdentity = trait.Format().Identity()
		} else {
			trait, present := mediaformat.WriteOf(component)
			if !present {
				result[index] = bound.AutomaticSink(projection, entry.Reference(), entry.SinkTrait())
				continue
			}
			if !trait.Valid() {
				return nil, missingFormatRequirements(projection, node)
			}
			selected, err := selectSinkCapabilities(entry, projection, node, trait, policy)
			if err != nil {
				return nil, err
			}
			result[index] = selected
			continue
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
		result[index] = bound.ResolveSource(entry, projection)
	}
	return result, nil
}

// FormatNode returns the Format node selected for an Access boundary. An
// anchored source already identifies that node; carrier boundaries discover
// it from their one adjacent edge.
func FormatNode(entry bound.Entry, edges []job.Edge) (job.NodeID, error) {
	if anchor := entry.Anchor(); anchor.Valid() {
		return anchor, nil
	}
	return AdjacentBoundaryNode(entry.Projection(), edges)
}

// FinalizeOutput selects the sink capabilities required by the Format chosen
// before solving. Output acquisition still occurs after solving and resource
// reservation.
func FinalizeOutput(entry bound.Entry, component plugin.Component, policy job.ResourcePolicy) (bound.Entry, error) {
	projection := entry.Projection()
	trait, ok := mediaformat.WriteOf(component)
	if !entry.Pending() || projection.Direction != plan.OutputBoundary || projection.Kind != plan.ProviderBoundary || !ok || !trait.Valid() {
		return bound.Entry{}, diagnostic.NewError(bindItem("bind.format-selection", component.Identity(), "automatic Format selection cannot finalize the Access output", map[string]string{"node": projection.Node}))
	}
	node := job.NewNode(job.NodeID("format@"+projection.Node), component.Identity(), config.NewPatch())
	return selectSinkCapabilities(entry, projection, node, trait, policy)
}

func selectSinkCapabilities(entry bound.Entry, projection plan.Boundary, node job.Node, trait mediaformat.WriteTrait, policy job.ResourcePolicy) (bound.Entry, error) {
	available, err := access.NewCapabilities(projection.Available...)
	if err != nil {
		return bound.Entry{}, err
	}
	selection, selected := access.Select(available, trait.Requirements())
	if !selected {
		adapted, adaptedSelection, possible := spoolCapabilities(available, trait.Requirements())
		if !possible {
			return bound.Entry{}, unsatisfiedCapabilities(projection, node, trait.Format().Identity(), trait.Requirements())
		}
		if !policy.AllowSpool {
			return bound.Entry{}, spoolDisabled(projection, node, trait.Format().Identity(), trait.Requirements())
		}
		spec, err := access.NewSpoolSpec(int64(policy.SpoolMaxBytes), 0, policy.SpoolStorage, 0, true, entry.SinkTrait().TransactionClass())
		if err != nil {
			return bound.Entry{}, err
		}
		projection.Effective = adapted.Values()
		projection.Selected = adaptedSelection.Capabilities()
		projection.Spool = spec
		return bound.Sink(projection, entry.Reference(), entry.SinkTrait()), nil
	}
	projection.Selected = selection.Capabilities()
	return bound.Sink(projection, entry.Reference(), entry.SinkTrait()), nil
}

func probeSelection(available access.Capabilities) (access.Selection, bool) {
	for _, capability := range []access.Capability{access.RandomRead, access.SequentialRead} {
		selection, ok := access.Select(available, access.NewRequirements(access.AllOf(capability)))
		if ok {
			return selection, true
		}
	}
	return access.Selection{}, false
}

// FinalizeInput replaces an automatic boundary's Probe acquisition selection
// with the requirements of the Format selected during Prepare.
func FinalizeInput(entry bound.Entry, node job.Node, component plugin.Component, actual access.Capabilities) (bound.Entry, access.Selection, error) {
	projection := entry.Projection()
	trait, ok := mediaformat.ReadOf(component)
	if !entry.Pending() || projection.Direction != plan.InputBoundary || projection.Kind != plan.ProviderBoundary || !ok || !trait.Valid() || !actual.Valid() {
		return bound.Entry{}, access.Selection{}, diagnostic.NewError(bindItem("bind.format-selection", component.Identity(), "automatic Format selection cannot finalize the Access input", map[string]string{"node": node.ID().String()}))
	}
	available, err := access.NewCapabilities(projection.Effective...)
	if err != nil {
		return bound.Entry{}, access.Selection{}, err
	}
	for _, capability := range available.Values() {
		if !actual.Contains(capability) {
			return bound.Entry{}, access.Selection{}, diagnostic.NewError(bindItem("bind.session-capability", component.Identity(), "acquired Access session no longer provides a declared capability", map[string]string{
				"node": node.ID().String(), "capability": string(capability),
			}))
		}
	}
	selection, selected := access.Select(available, trait.Requirements())
	if !selected {
		return bound.Entry{}, access.Selection{}, unsatisfiedCapabilities(projection, node, trait.Format().Identity(), trait.Requirements())
	}
	projection.Selected = selection.Capabilities()
	return bound.ResolveSource(entry, projection), selection, nil
}

func spoolCapabilities(available access.Capabilities, requirements access.Requirements) (access.Capabilities, access.Selection, bool) {
	if _, ok := access.Select(available, requirements); ok {
		return access.Capabilities{}, access.Selection{}, false
	}
	if !available.Contains(access.SequentialWrite) || available.Contains(access.RandomWrite) {
		return access.Capabilities{}, access.Selection{}, false
	}
	values := append(available.Values(), access.RandomWrite)
	adapted, err := access.NewCapabilities(values...)
	if err != nil {
		return access.Capabilities{}, access.Selection{}, false
	}
	selection, ok := access.Select(adapted, requirements)
	return adapted, selection, ok
}

// AdjacentBoundaryNode returns the one explicit component connected directly
// to an Access boundary. Prepare uses the same relation selected during Bind
// when it attaches node-local inspection state.
func AdjacentBoundaryNode(boundary plan.Boundary, edges []job.Edge) (job.NodeID, error) {
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

func probeCapabilityUnavailable(boundary plan.Boundary) error {
	return diagnostic.NewError(bindItem("bind.probe-capability", plugin.Identity{}, "Access source provides neither position-independent nor sequential reading for Format Probe", map[string]string{
		"node": boundary.Node, "scheme": boundary.Scheme, "direction": "read", "available": capabilityList(boundary.Available),
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

func spoolDisabled(boundary plan.Boundary, adjacent job.Node, formatIdentity plugin.Identity, requirements access.Requirements) error {
	return diagnostic.NewError(bindItem("bind.spool-disabled", adjacent.Component(), "Access sink requires capability adaptation but spool is disabled by policy", map[string]string{
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
