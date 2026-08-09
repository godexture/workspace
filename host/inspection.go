package host

import (
	"context"
	"errors"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (h *Host) inspectInputs(ctx context.Context, request job.Job, entries []bound.Entry, sessions []acquiredSession) (graph.CompileContexts, error) {
	requested, ok := request.Graph()
	if !ok {
		return graph.CompileContexts{}, errors.New("normalized Job has no graph to inspect")
	}
	nodes := make(map[job.NodeID]job.Node, len(requested.Nodes()))
	for _, node := range requested.Nodes() {
		nodes[node.ID()] = node
	}
	openings := make(map[string]access.Opening, len(sessions))
	for _, session := range sessions {
		openings[session.node] = session.opening
	}
	contexts := make(map[job.NodeID]plugin.CompileContext)
	for _, entry := range entries {
		projection := entry.Projection()
		if projection.Direction != plan.InputBoundary || projection.Kind == plan.EndpointBoundary {
			continue
		}
		adjacent, err := bind.AdjacentBoundaryNode(projection, requested.Edges())
		if err != nil {
			return graph.CompileContexts{}, err
		}
		node, ok := nodes[adjacent]
		if !ok {
			return graph.CompileContexts{}, inspectDiagnostic("prepare.inspect-node", projection, plugin.Identity{}, "Access input has no adjacent Format node", nil)
		}
		component, ok := h.index.Lookup(node.Component())
		if !ok {
			return graph.CompileContexts{}, inspectDiagnostic("prepare.inspect-component", projection, node.Component(), "adjacent Format component is absent from the Host catalog", nil)
		}
		trait, present := mediaformat.ReadOf(component)
		if !present || !trait.Valid() {
			if projection.Kind == plan.DirectBoundary {
				continue
			}
			return graph.CompileContexts{}, inspectDiagnostic("prepare.inspect-trait", projection, component.Identity(), "Access input has no valid Format read trait", nil)
		}
		if !trait.HasInspect() {
			continue
		}
		opening, openingOK := openings[projection.Node]
		if projection.Kind == plan.DirectBoundary {
			opening, openingOK = entry.DirectOpening().(access.Opening)
		}
		if !openingOK || !opening.Valid() || opening.Direction() != access.SourceDirection {
			return graph.CompileContexts{}, inspectDiagnostic("prepare.inspect-opening", projection, component.Identity(), "Format Inspect requires a selected Access source opening", nil)
		}
		var inspection mediaformat.Inspection
		failure := invoke(ctx, PreparePhase, adjacent.String(), "format/inspect", func(callContext context.Context) error {
			var inspectErr error
			inspection, inspectErr = trait.Inspect(mediaformat.NewInspectContext(callContext, opening))
			return inspectErr
		})
		if failure != nil {
			return graph.CompileContexts{}, *failure
		}
		compileContext, err := mediaformat.WithInspection(contexts[adjacent], inspection)
		if err != nil {
			return graph.CompileContexts{}, inspectDiagnostic("prepare.inspect-result", projection, component.Identity(), "Format returned an invalid or duplicate inspection", map[string]string{"cause": err.Error()})
		}
		contexts[adjacent] = compileContext
	}
	return graph.NewCompileContexts(contexts), nil
}

func inspectDiagnostic(code string, boundary plan.Boundary, component plugin.Identity, message string, detail map[string]string) error {
	if detail == nil {
		detail = make(map[string]string)
	}
	detail["boundary"] = boundary.Node
	detail["formatNode"] = component.String()
	return diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: component.String()}, message, detail))
}
