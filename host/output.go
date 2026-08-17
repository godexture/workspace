package host

import (
	"errors"
	"strconv"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/solve"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (h *Host) selectOutputFormats(selected inputSelection) (inputSelection, error) {
	result := selected
	result.entries = append([]bound.Entry(nil), selected.entries...)
	requested, ok := result.request.Graph()
	if !ok {
		return inputSelection{}, errors.New("normalized Job has no graph for output Format selection")
	}
	inspectedByNode := indexInspections(result.inspected)
	upstream := reverseNodeAdjacency(requested.Edges())
	outputs := result.request.Outputs()
	var terminals []solve.TerminalSelection
	for entryIndex, entry := range result.entries {
		projection := entry.Projection()
		if !entry.Pending() || projection.Direction != plan.OutputBoundary {
			continue
		}
		if projection.Choice < 0 || projection.Choice >= len(outputs) {
			return inputSelection{}, formatSelectionDiagnostic("prepare.format-choice", projection, plugin.Identity{}, "output boundary has no corresponding Job choice", nil)
		}
		selector, requested := outputs[projection.Choice].FormatRequest()
		if !requested {
			inputFormat, err := h.defaultOutputFormat(result.request, result.entries)
			if err != nil {
				return inputSelection{}, err
			}
			selector, err = job.SelectFormat(inputFormat)
			if err != nil {
				return inputSelection{}, err
			}
		}
		match, err := h.resolveWriteFormat(projection, selector)
		if err != nil {
			return inputSelection{}, err
		}
		patch, configured := selector.Config()
		if configured {
			if _, err := match.Component().Resolve(patch); err != nil {
				return inputSelection{}, err
			}
		}
		resolved, err := bind.FinalizeOutput(entry, match.Component(), result.request.Policy().Resources)
		if err != nil {
			return inputSelection{}, err
		}
		prepared, _, err := h.formatCompileContext(match.Component())
		if err != nil {
			return inputSelection{}, diagnostic.NewError(diagnostic.NewItem(
				"prepare.metadata-resolver", diagnostic.ErrorSeverity, diagnostic.Path{Component: match.Component().Identity().String()}, "output Format metadata bindings could not be resolved",
				map[string]string{"boundary": projection.Node, "cause": err.Error()},
			))
		}
		values := upstreamInspections(job.NodeID(projection.Node), match.Format(), upstream, inspectedByNode)
		switch len(values) {
		case 0:
		case 1:
			prepared, err = mediaformat.WithInspection(prepared, values[0].value)
			if err != nil {
				return inputSelection{}, inspectHandoffDiagnostic(match.Component().Identity(), map[string]string{
					"format":    match.Format().Identity().String(),
					"source":    values[0].source.String(),
					"writeNode": projection.Node,
					"cause":     err.Error(),
				}, "writable Format CompileContext already contains a different inspection")
			}
		default:
			return inputSelection{}, ambiguousInspection(writeFormatNode{id: job.NodeID(projection.Node), component: match.Component(), format: match.Format()}, values)
		}
		result.entries[entryIndex] = resolved
		terminals = append(terminals, solve.TerminalSelection{
			Boundary: job.At(job.NodeID(projection.Node), projection.Port), Component: match.Component().Identity(), Config: patch, Configured: configured, Context: prepared, Reason: "format.output",
		})
	}
	preselection, err := result.preselection.WithTerminals(terminals...)
	if err != nil {
		return inputSelection{}, err
	}
	result.preselection = preselection
	return result, nil
}

func (h *Host) defaultOutputFormat(request job.Job, entries []bound.Entry) (mediaformat.Format, error) {
	requested, ok := request.Graph()
	if !ok {
		return mediaformat.Format{}, errors.New("normalized Job has no graph for output Format selection")
	}
	nodes := make(map[job.NodeID]job.Node, len(requested.Nodes()))
	for _, node := range requested.Nodes() {
		nodes[node.ID()] = node
	}
	var selected []mediaformat.Format
	for _, entry := range entries {
		projection := entry.Projection()
		if projection.Direction != plan.InputBoundary || entry.Pending() {
			continue
		}
		adjacent, err := bind.FormatNode(entry, requested.Edges())
		if err != nil {
			return mediaformat.Format{}, err
		}
		node, ok := nodes[adjacent]
		if !ok {
			continue
		}
		component, ok := h.index.Lookup(node.Component())
		if !ok {
			continue
		}
		trait, ok := mediaformat.ReadOf(component)
		if ok && trait.Valid() {
			selected = append(selected, trait.Format())
		}
	}
	if len(selected) != 1 {
		return mediaformat.Format{}, diagnostic.NewError(diagnostic.NewItem(
			"prepare.output-format-default", diagnostic.ErrorSeverity, diagnostic.Path{}, "output Format was omitted but exactly one selected input Format is not available",
			map[string]string{"inputs": strconv.Itoa(len(selected))},
		))
	}
	return selected[0], nil
}
