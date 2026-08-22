package bind

import (
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (r Registry) validatePinnedFormats(inputs []job.Input, outputs []job.Output, nodes []job.Node, edges []job.Edge, entries []bound.Entry) error {
	byID := make(map[job.NodeID]job.Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID()] = node
	}
	for _, entry := range entries {
		projection := entry.Projection()
		selector, present := boundarySelector(projection, inputs, outputs)
		if !present {
			continue
		}
		adjacent, err := FormatNode(entry, edges)
		if err != nil {
			return err
		}
		node, ok := byID[adjacent]
		if !ok {
			continue
		}
		component, ok := r.index.Lookup(node.Component())
		if !ok {
			continue
		}
		value, ok := componentFormat(component, projection.Direction)
		if !ok {
			continue
		}
		if !selector.Matches(value) {
			return diagnostic.NewError(formatSelectorItem("bind.format-conflict", projection, component.Identity(), "Format selector conflicts with the pinned boundary component", selector, map[string]string{
				"pinnedFormat": value.Identity().String(),
			}))
		}
		patch, configured := selector.Config()
		if !configured {
			continue
		}
		selected, err := component.Resolve(patch)
		if err != nil {
			return err
		}
		pinned, err := component.Resolve(node.Config())
		if err != nil {
			return err
		}
		if selected.Fingerprint() != pinned.Fingerprint() {
			return diagnostic.NewError(formatSelectorItem("bind.format-config-conflict", projection, component.Identity(), "Format selector config conflicts with the pinned component config", selector, map[string]string{
				"selectedConfig": selected.Fingerprint().String(), "pinnedConfig": pinned.Fingerprint().String(),
			}))
		}
	}
	return nil
}

func boundarySelector(boundary plan.Boundary, inputs []job.Input, outputs []job.Output) (job.FormatSelector, bool) {
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

func componentFormat(component plugin.Component, direction plan.BoundaryDirection) (mediaformat.Format, bool) {
	if direction == plan.InputBoundary {
		trait, ok := mediaformat.ReadOf(component)
		return trait.Format(), ok && trait.Valid()
	}
	trait, ok := mediaformat.WriteOf(component)
	return trait.Format(), ok && trait.Valid()
}

func formatSelectorItem(code string, boundary plan.Boundary, component plugin.Identity, message string, selector job.FormatSelector, detail map[string]string) diagnostic.Item {
	if detail == nil {
		detail = make(map[string]string)
	}
	detail["boundary"] = boundary.Node
	detail["direction"] = boundaryDirection(boundary.Direction)
	detail["selector"] = selector.String()
	return diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: component.String()}, message, detail)
}
