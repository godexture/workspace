package graph

import (
	"fmt"
	"sort"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/gotype"
	"github.com/godexture/godec/job"
)

type shapedNode struct {
	request        job.Node
	componentIndex int
	shape          flow.Shape
}

func validateTopology(nodes []shapedNode, edges []job.Edge) ([]int, []diagnostic.Item) {
	return validateTopologyMode(nodes, edges, false)
}

func validateTopologyMode(nodes []shapedNode, edges []job.Edge, allowSchemaGaps bool) ([]int, []diagnostic.Item) {
	byID := make(map[job.NodeID]int, len(nodes))
	for index, node := range nodes {
		byID[node.request.ID()] = index
	}
	inputCounts := make([]map[string]int, len(nodes))
	outputCounts := make([]map[string]int, len(nodes))
	var items []diagnostic.Item
	for index := range nodes {
		inputCounts[index] = make(map[string]int)
		outputCounts[index] = make(map[string]int)
	}
	for _, edge := range edges {
		fromIndex, fromOK := byID[edge.From().Node()]
		toIndex, toOK := byID[edge.To().Node()]
		if !fromOK || !toOK {
			continue
		}
		fromPort, fromPortOK := findPort(nodes[fromIndex].shape.Outputs, edge.From().ID())
		toPort, toPortOK := findPort(nodes[toIndex].shape.Inputs, edge.To().ID())
		if !fromPortOK {
			items = append(items, graphItem("graph.unknown-output", edge.From(), "edge names an unknown output port", nil))
		}
		if !toPortOK {
			items = append(items, graphItem("graph.unknown-input", edge.To(), "edge names an unknown input port", nil))
		}
		if !fromPortOK || !toPortOK {
			continue
		}
		if !allowSchemaGaps && (fromPort.Schema().Identity() != toPort.Schema().Identity() || fromPort.Schema().Payload() != toPort.Schema().Payload()) {
			items = append(items, graphItem("graph.schema-mismatch", edge.To(), "connected ports declare different schemas", map[string]string{
				"source":        fromPort.Schema().Identity().String(),
				"sourcePayload": gotype.Canonical(fromPort.Schema().Payload()),
				"target":        toPort.Schema().Identity().String(),
				"targetPayload": gotype.Canonical(toPort.Schema().Payload()),
			}))
		}
		outputCounts[fromIndex][fromPort.ID()]++
		inputCounts[toIndex][toPort.ID()]++
		if outputCounts[fromIndex][fromPort.ID()] > 1 && fromPort.Multiplicity() != flow.ManyMultiplicity {
			items = append(items, graphItem("graph.fan-out", edge.From(), "output port does not permit multiple connections", nil))
		}
		if inputCounts[toIndex][toPort.ID()] > 1 && toPort.Multiplicity() != flow.ManyMultiplicity {
			items = append(items, graphItem("graph.fan-in", edge.To(), "input port does not permit multiple connections", nil))
		}
	}
	for index, node := range nodes {
		for _, port := range node.shape.Inputs {
			if port.Required() && inputCounts[index][port.ID()] == 0 {
				items = append(items, graphItem("graph.required-input", job.At(node.request.ID(), port.ID()), "required input port is not connected", nil))
			}
		}
		for _, port := range node.shape.Outputs {
			if port.Required() && outputCounts[index][port.ID()] == 0 {
				items = append(items, graphItem("graph.required-output", job.At(node.request.ID(), port.ID()), "required output port is not connected", nil))
			}
		}
	}

	order, cyclic := topologicalOrder(nodes, edges, byID)
	if cyclic {
		items = append(items, diagnostic.NewItem("graph.cycle", diagnostic.ErrorSeverity, diagnostic.Path{}, "requested graph contains a cycle", nil))
	}
	items = append(items, validateReachability(nodes, edges, byID)...)
	return order, items
}

func topologicalOrder(nodes []shapedNode, edges []job.Edge, byID map[job.NodeID]int) ([]int, bool) {
	indegree := make([]int, len(nodes))
	outgoing := make([][]int, len(nodes))
	for _, edge := range edges {
		from, fromOK := byID[edge.From().Node()]
		to, toOK := byID[edge.To().Node()]
		if !fromOK || !toOK {
			continue
		}
		outgoing[from] = append(outgoing[from], to)
		indegree[to]++
	}
	ready := make([]int, 0, len(nodes))
	for index, degree := range indegree {
		if degree == 0 {
			ready = append(ready, index)
		}
	}
	sort.Slice(ready, func(left, right int) bool {
		return nodes[ready[left]].request.ID().String() < nodes[ready[right]].request.ID().String()
	})
	order := make([]int, 0, len(nodes))
	for len(ready) != 0 {
		current := ready[0]
		ready = ready[1:]
		order = append(order, current)
		for _, next := range outgoing[current] {
			indegree[next]--
			if indegree[next] == 0 {
				ready = append(ready, next)
				sort.Slice(ready, func(left, right int) bool {
					return nodes[ready[left]].request.ID().String() < nodes[ready[right]].request.ID().String()
				})
			}
		}
	}
	return order, len(order) != len(nodes)
}

func validateReachability(nodes []shapedNode, edges []job.Edge, byID map[job.NodeID]int) []diagnostic.Item {
	forward := make([][]int, len(nodes))
	reverse := make([][]int, len(nodes))
	for _, edge := range edges {
		from, fromOK := byID[edge.From().Node()]
		to, toOK := byID[edge.To().Node()]
		if fromOK && toOK {
			forward[from] = append(forward[from], to)
			reverse[to] = append(reverse[to], from)
		}
	}
	var sources, sinks []int
	for index, node := range nodes {
		if len(node.shape.Inputs) == 0 {
			sources = append(sources, index)
		}
		if len(node.shape.Outputs) == 0 {
			sinks = append(sinks, index)
		}
	}
	var items []diagnostic.Item
	if len(sources) == 0 {
		items = append(items, diagnostic.NewItem("graph.no-source", diagnostic.ErrorSeverity, diagnostic.Path{}, "requested graph has no source node", nil))
	}
	if len(sinks) == 0 {
		items = append(items, diagnostic.NewItem("graph.no-sink", diagnostic.ErrorSeverity, diagnostic.Path{}, "requested graph has no sink node", nil))
	}
	reachedFromSource := walk(sources, forward)
	reachesSink := walk(sinks, reverse)
	for index, node := range nodes {
		if !reachedFromSource[index] {
			items = append(items, diagnostic.NewItem("graph.unreachable", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.request.ID().String()}, "node is unreachable from every source", nil))
		}
		if !reachesSink[index] {
			items = append(items, diagnostic.NewItem("graph.no-sink-path", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.request.ID().String()}, "node cannot reach any sink", nil))
		}
	}
	return items
}

func walk(starts []int, adjacency [][]int) []bool {
	visited := make([]bool, len(adjacency))
	queue := append([]int(nil), starts...)
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true
		queue = append(queue, adjacency[current]...)
	}
	return visited
}

func findPort(ports []flow.Port, id string) (flow.Port, bool) {
	for _, port := range ports {
		if port.ID() == id {
			return port, true
		}
	}
	return flow.Port{}, false
}

func graphItem(code string, port job.Port, message string, detail map[string]string) diagnostic.Item {
	return diagnostic.NewItem(code, diagnostic.ErrorSeverity, diagnostic.Path{Component: port.Node().String(), Descriptor: port.ID()}, message, detail)
}

func errUnknownNode(id job.NodeID) error {
	return fmt.Errorf("graph node %q is not present", id)
}
