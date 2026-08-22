package host

import (
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (h *Host) validatePinnedFormatSelectors(request job.Job, entries []bound.Entry) error {
	requested, ok := request.Graph()
	if !ok {
		return nil
	}
	nodes := make(map[job.NodeID]job.Node, len(requested.Nodes()))
	for _, node := range requested.Nodes() {
		nodes[node.ID()] = node
	}
	inputs, outputs := request.Inputs(), request.Outputs()
	for _, entry := range entries {
		if entry.Pending() {
			continue
		}
		projection := entry.Projection()
		selector, present := boundaryFormatSelector(projection, inputs, outputs)
		if !present {
			continue
		}
		adjacent, err := bind.FormatNode(entry, requested.Edges())
		if err != nil {
			return err
		}
		node, nodeOK := nodes[adjacent]
		component, componentOK := h.index.Lookup(node.Component())
		if !nodeOK || !componentOK {
			return pinnedFormatDiagnostic("prepare.format-pin", projection, plugin.Identity{}, "Format selector has no pinned adjacent component", selector, nil)
		}
		if _, formatOK := directionalFormat(component, projection.Direction); formatOK {
			continue
		}
		detail := map[string]string{"component": component.Identity().String()}
		if projection.Kind == plan.DirectBoundary {
			detail["milestone"] = "M9"
		}
		return pinnedFormatDiagnostic("prepare.format-pin", projection, component.Identity(), "Format selector requires a matching pinned Format component at this boundary", selector, detail)
	}
	return nil
}

func boundaryFormatSelector(boundary plan.Boundary, inputs []job.Input, outputs []job.Output) (job.FormatSelector, bool) {
	if boundary.Choice < 0 {
		return job.FormatSelector{}, false
	}
	if boundary.Direction == plan.InputBoundary && boundary.Choice < len(inputs) {
		return inputs[boundary.Choice].FormatHint()
	}
	if boundary.Direction == plan.OutputBoundary && boundary.Choice < len(outputs) {
		return outputs[boundary.Choice].FormatRequest()
	}
	return job.FormatSelector{}, false
}

func directionalFormat(component plugin.Component, direction plan.BoundaryDirection) (mediaformat.Format, bool) {
	if direction == plan.InputBoundary {
		trait, ok := mediaformat.ReadOf(component)
		return trait.Format(), ok && trait.Valid()
	}
	trait, ok := mediaformat.WriteOf(component)
	return trait.Format(), ok && trait.Valid()
}

func pinnedFormatDiagnostic(code string, boundary plan.Boundary, component plugin.Identity, message string, selector job.FormatSelector, detail map[string]string) error {
	if detail == nil {
		detail = make(map[string]string)
	}
	detail["boundary"] = boundary.Node
	detail["direction"] = boundaryDirectionLabel(boundary.Direction)
	detail["selector"] = selector.String()
	path := diagnostic.Path{}
	if !component.IsZero() {
		path.Component = component.String()
	}
	return diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, path, message, detail))
}

func boundaryDirectionLabel(direction plan.BoundaryDirection) string {
	if direction == plan.InputBoundary {
		return "read"
	}
	return "write"
}
