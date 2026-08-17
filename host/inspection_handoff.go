package host

import (
	"sort"
	"strconv"
	"strings"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin"
)

type inspectedFormat struct {
	source job.NodeID
	value  mediaformat.Inspection
}

type writeFormatNode struct {
	node      job.Node
	component plugin.Component
	format    mediaformat.Format
}

func (h *Host) handoffInspections(requested job.Graph, contexts map[job.NodeID]plugin.CompileContext, inspected []inspectedFormat) error {
	if len(inspected) == 0 {
		return nil
	}
	writers := make([]writeFormatNode, 0, len(requested.Nodes()))
	for _, node := range requested.Nodes() {
		component, ok := h.index.Lookup(node.Component())
		if !ok {
			continue
		}
		trait, ok := mediaformat.WriteOf(component)
		if !ok || !trait.Valid() {
			continue
		}
		writers = append(writers, writeFormatNode{node: node, component: component, format: trait.Format()})
	}
	if len(writers) == 0 {
		return nil
	}
	sort.Slice(writers, func(left, right int) bool {
		return writers[left].node.ID().String() < writers[right].node.ID().String()
	})

	inspectedByNode := make(map[job.NodeID]inspectedFormat, len(inspected))
	for _, value := range inspected {
		inspectedByNode[value.source] = value
	}
	upstream := reverseNodeAdjacency(requested.Edges())
	candidates := make(map[job.NodeID][]inspectedFormat, len(writers))
	for _, writer := range writers {
		values := upstreamInspections(writer.node.ID(), writer.format, upstream, inspectedByNode)
		candidates[writer.node.ID()] = values
		if len(values) < 2 {
			continue
		}
		sources := make([]string, len(values))
		for index, value := range values {
			sources[index] = value.source.String()
		}
		sort.Strings(sources)
		return inspectHandoffDiagnostic(writer.component.Identity(), map[string]string{
			"format":      writer.format.Identity().String(),
			"sources":     strings.Join(sources, ","),
			"writeNode":   writer.node.ID().String(),
			"sourceCount": strconv.Itoa(len(values)),
		}, "multiple inspected inputs cannot be handed off to one writable Format")
	}

	for _, writer := range writers {
		values := candidates[writer.node.ID()]
		if len(values) != 1 {
			continue
		}
		if writer.node.ID() == values[0].source {
			continue
		}
		context := contexts[writer.node.ID()]
		prepared, err := mediaformat.WithInspection(context, values[0].value)
		if err != nil {
			return inspectHandoffDiagnostic(writer.component.Identity(), map[string]string{
				"format":    writer.format.Identity().String(),
				"source":    values[0].source.String(),
				"writeNode": writer.node.ID().String(),
				"cause":     err.Error(),
			}, "writable Format CompileContext already contains a different inspection")
		}
		contexts[writer.node.ID()] = prepared
	}
	return nil
}

func reverseNodeAdjacency(edges []job.Edge) map[job.NodeID][]job.NodeID {
	result := make(map[job.NodeID][]job.NodeID, len(edges))
	for _, edge := range edges {
		result[edge.To().Node()] = append(result[edge.To().Node()], edge.From().Node())
	}
	return result
}

func upstreamInspections(writer job.NodeID, value mediaformat.Format, upstream map[job.NodeID][]job.NodeID, inspected map[job.NodeID]inspectedFormat) []inspectedFormat {
	if !writer.Valid() || !value.Valid() {
		return nil
	}
	stack := []job.NodeID{writer}
	visited := make(map[job.NodeID]struct{})
	var result []inspectedFormat
	for len(stack) != 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if _, ok := visited[node]; ok {
			continue
		}
		visited[node] = struct{}{}
		if candidate, ok := inspected[node]; ok && candidate.value.Format().Identity() == value.Identity() {
			result = append(result, candidate)
		}
		stack = append(stack, upstream[node]...)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].source.String() < result[right].source.String()
	})
	return result
}

func inspectHandoffDiagnostic(component plugin.Identity, detail map[string]string, message string) error {
	return diagnostic.NewError(diagnostic.NewItem("prepare.inspect-handoff", diagnostic.ErrorSeverity, diagnostic.Path{Component: component.String()}, message, detail))
}
