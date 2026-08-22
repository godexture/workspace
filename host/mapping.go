package host

import (
	"errors"
	"strconv"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/bind"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/solve"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (h *Host) prepareMappings(selected inputSelection) (inputSelection, error) {
	result := selected
	requested, ok := result.request.Graph()
	if !ok {
		return inputSelection{}, errors.New("normalized Job has no graph for stream mapping")
	}
	nodes := make(map[job.NodeID]job.Node, len(requested.Nodes()))
	for _, node := range requested.Nodes() {
		nodes[node.ID()] = node
	}
	for _, entry := range result.entries {
		boundary := entry.Projection()
		if boundary.Kind == plan.EndpointBoundary {
			continue
		}
		id, err := bind.FormatNode(entry, requested.Edges())
		if err != nil {
			return inputSelection{}, err
		}
		node, exists := nodes[id]
		if !exists {
			return inputSelection{}, mappingDiagnostic("prepare.mapping-node", boundary, plugin.Identity{}, "stream mapping boundary has no selected Format node", nil)
		}
		component, exists := h.index.Lookup(node.Component())
		if !exists {
			return inputSelection{}, mappingDiagnostic("prepare.mapping-component", boundary, node.Component(), "selected Format component is absent from the Host catalog", nil)
		}
		if _, formatOK := directionalFormat(component, boundary.Direction); !formatOK {
			continue
		}
		result.formats = append(result.formats, solve.SelectedFormat{Direction: boundary.Direction, Choice: boundary.Choice, Node: id})
	}

	mappings := result.request.Mappings()
	if len(mappings) == 0 {
		return result, nil
	}
	input, inputOK := selectedFormat(result.formats, plan.InputBoundary, 0)
	_, outputOK := selectedFormat(result.formats, plan.OutputBoundary, 0)
	if !inputOK || !outputOK {
		return inputSelection{}, mappingDiagnostic("prepare.mapping-boundary", plan.Boundary{}, plugin.Identity{}, "exact stream mapping requires one selected input and output Format", map[string]string{
			"input": strconv.FormatBool(inputOK), "output": strconv.FormatBool(outputOK),
		})
	}
	inputNode := nodes[input]
	component, exists := h.index.Lookup(inputNode.Component())
	if !exists {
		return inputSelection{}, mappingDiagnostic("prepare.mapping-component", plan.Boundary{}, inputNode.Component(), "selected input Format component is absent from the Host catalog", nil)
	}
	read, exists := mediaformat.ReadOf(component)
	if !exists || !read.Valid() {
		return inputSelection{}, mappingDiagnostic("prepare.mapping-reader", plan.Boundary{}, component.Identity(), "exact stream mapping requires a valid input Format reader", nil)
	}
	ids := make([]stream.ID, len(mappings))
	for index, mapping := range mappings {
		if mapping.Input() != 0 || mapping.Output() != 0 {
			return inputSelection{}, mappingDiagnostic("prepare.mapping-boundary", plan.Boundary{}, component.Identity(), "M7 exact stream mapping supports only input 0 and output 0", nil)
		}
		ids[index] = mapping.Stream()
	}
	selection, err := mediaformat.NewSelection(read.Format(), ids...)
	if err != nil {
		return inputSelection{}, mappingDiagnostic("prepare.mapping-selection", plan.Boundary{}, component.Identity(), "exact stream mapping is invalid", map[string]string{"cause": err.Error()})
	}
	result.contexts, err = withMappingSelection(result.contexts, input, selection)
	if err != nil {
		return inputSelection{}, mappingDiagnostic("prepare.mapping-selection", plan.Boundary{}, component.Identity(), "input Format CompileContext rejected exact stream mapping", map[string]string{"cause": err.Error()})
	}
	return result, nil
}

func withMappingSelection(contexts graph.CompileContexts, node job.NodeID, selection mediaformat.Selection) (graph.CompileContexts, error) {
	prepared, err := mediaformat.WithSelection(contexts.For(node), selection)
	if err != nil {
		return graph.CompileContexts{}, err
	}
	return contexts.WithPrepared(node, prepared), nil
}

func selectedFormat(values []solve.SelectedFormat, direction plan.BoundaryDirection, choice int) (job.NodeID, bool) {
	for _, value := range values {
		if value.Direction == direction && value.Choice == choice {
			return value.Node, true
		}
	}
	return "", false
}

func mappingDiagnostic(code string, boundary plan.Boundary, component plugin.Identity, message string, detail map[string]string) error {
	if detail == nil {
		detail = make(map[string]string)
	}
	if boundary.Node != "" {
		detail["boundary"] = boundary.Node
	}
	return diagnostic.NewError(diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: component.String()}, message, detail))
}
