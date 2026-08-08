package graph

import (
	"sort"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

// Compile resolves config and dynamic shapes, invokes each component Compile
// in topological order, and validates the complete explicit graph without
// opening an operator.
func Compile(index catalog.Index, requested job.Graph) (Graph, error) {
	if !requested.Valid() {
		return Graph{}, diagnostic.NewError(diagnostic.NewItem("graph.invalid-request", diagnostic.ErrorSeverity, diagnostic.Path{}, "requested graph is invalid", nil))
	}
	requests := requested.Nodes()
	edges := requested.Edges()
	sortRequested(requests, edges)
	shaped := make([]shapedNode, 0, len(requests))
	components := make([]plugin.Component, 0, len(requests))
	configs := make([]config.ResolvedView, 0, len(requests))
	var items []diagnostic.Item
	for _, request := range requests {
		component, ok := index.Lookup(request.Component())
		if !ok {
			items = append(items, diagnostic.NewItem("graph.component", diagnostic.ErrorSeverity, diagnostic.Path{Component: request.ID().String()}, "requested component is not in the Host catalog", map[string]string{"identity": request.Component().String()}))
			continue
		}
		resolved, err := component.Resolve(request.Config())
		if err != nil {
			items = append(items, prefixNode(errorItems(err), request.ID())...)
			continue
		}
		shape, err := component.Shape(plugin.ShapeContext{}, resolved)
		if err != nil {
			items = append(items, prefixNode(errorItems(err), request.ID())...)
			continue
		}
		componentIndex := len(components)
		components = append(components, component)
		configs = append(configs, resolved)
		shaped = append(shaped, shapedNode{request: request, componentIndex: componentIndex, shape: shape})
	}
	if len(items) != 0 {
		return Graph{}, diagnostic.NewError(items...)
	}
	order, topologyItems := validateTopology(shaped, edges)
	if len(topologyItems) != 0 {
		return Graph{}, diagnostic.NewError(topologyItems...)
	}

	compiledByID := make(map[job.NodeID]Node, len(shaped))
	for _, shapedIndex := range order {
		node := shaped[shapedIndex]
		inputs, inputItems := inputsFor(node, edges, compiledByID)
		items = append(items, inputItems...)
		if len(inputItems) != 0 {
			continue
		}
		configValue := configs[node.componentIndex]
		compilation, err := plugin.Compile(components[node.componentIndex], plugin.CompileContext{}, configValue, inputs)
		if err != nil {
			items = append(items, prefixNode(errorItems(err), node.request.ID())...)
			continue
		}
		requirements, ok := plugin.RequirementsOf[stream.Descriptor](compilation)
		if !ok {
			items = append(items, diagnostic.NewItem("graph.requirement-type", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.request.ID().String()}, "component compilation returned incompatible requirements", nil))
			continue
		}
		for _, requirement := range requirements {
			items = append(items, diagnostic.NewItem("graph.requirement", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.request.ID().String(), Descriptor: requirement.Port()}, "component input requirement is not satisfied", map[string]string{"need": requirement.Need().Code()}))
		}
		outputs, ok := plugin.OutputsOf[stream.Descriptor](compilation)
		if !ok {
			items = append(items, diagnostic.NewItem("graph.output-type", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.request.ID().String()}, "component compilation returned incompatible outputs", nil))
			continue
		}
		items = append(items, validateCompiledOutputs(node, outputs)...)
		if len(requirements) != 0 || hasNodeErrors(items, node.request.ID()) {
			continue
		}
		compiledByID[node.request.ID()] = Node{
			id:          node.request.ID(),
			component:   components[node.componentIndex],
			config:      configValue,
			shape:       node.shape.Clone(),
			inputs:      inputs,
			compilation: compilation,
		}
	}
	if len(items) != 0 {
		return Graph{}, diagnostic.NewError(items...)
	}
	result := make([]Node, 0, len(order))
	for _, shapedIndex := range order {
		result = append(result, compiledByID[shaped[shapedIndex].request.ID()])
	}
	return newGraph(result, edges), nil
}

func inputsFor(node shapedNode, edges []job.Edge, compiled map[job.NodeID]Node) (flow.Descriptors[stream.Descriptor], []diagnostic.Item) {
	incoming := make([]job.Edge, 0)
	for _, edge := range edges {
		if edge.To().Node() == node.request.ID() {
			incoming = append(incoming, edge)
		}
	}
	sort.Slice(incoming, func(left, right int) bool {
		if incoming[left].To().ID() != incoming[right].To().ID() {
			return incoming[left].To().ID() < incoming[right].To().ID()
		}
		return incoming[left].From().String() < incoming[right].From().String()
	})
	var bindings []flow.PortDescriptor[stream.Descriptor]
	var items []diagnostic.Item
	for _, edge := range incoming {
		upstream, ok := compiled[edge.From().Node()]
		if !ok {
			items = append(items, graphItem("graph.blocked-input", edge.To(), "upstream node did not compile", map[string]string{"source": edge.From().Node().String()}))
			continue
		}
		values := upstream.Outputs().At(edge.From().ID())
		if len(values) == 0 {
			items = append(items, graphItem("graph.missing-descriptor", edge.From(), "compiled output has no descriptor for connected port", nil))
			continue
		}
		for _, descriptor := range values {
			bindings = append(bindings, flow.Describe(edge.To().ID(), descriptor))
		}
	}
	return flow.NewDescriptors(bindings...), items
}

func validateCompiledOutputs(node shapedNode, outputs flow.Descriptors[stream.Descriptor]) []diagnostic.Item {
	var items []diagnostic.Item
	if err := outputs.Validate(stream.Descriptor.Valid); err != nil {
		items = append(items, diagnostic.NewItem("graph.invalid-descriptor", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.request.ID().String()}, "component returned an invalid stream descriptor", map[string]string{"cause": err.Error()}))
	}
	for _, binding := range outputs.Bindings() {
		port, ok := findPort(node.shape.Outputs, binding.Port())
		if !ok {
			continue
		}
		if binding.Descriptor().Schema() != port.Schema().Identity() {
			items = append(items, graphItem("graph.schema-mismatch", job.At(node.request.ID(), binding.Port()), "compiled descriptor schema does not match output port", map[string]string{
				"declared": port.Schema().Identity().String(),
				"actual":   binding.Descriptor().Schema().String(),
			}))
		}
		if !binding.Descriptor().TimeBase().Valid() {
			items = append(items, graphItem("graph.time-base", job.At(node.request.ID(), binding.Port()), "compiled descriptor has no resolved time base", nil))
		}
	}
	return items
}

func prefixNode(items []diagnostic.Item, id job.NodeID) []diagnostic.Item {
	result := make([]diagnostic.Item, len(items))
	for index, item := range items {
		path := item.Path
		path.Component = id.String()
		result[index] = item.WithPath(path)
	}
	return result
}

func errorItems(err error) []diagnostic.Item {
	items := diagnostic.ItemsOf(err)
	if len(items) != 0 || err == nil {
		return items
	}
	return []diagnostic.Item{diagnostic.NewItem("diagnostic.wrapped-error", diagnostic.ErrorSeverity, diagnostic.Path{}, err.Error(), nil)}
}

func hasNodeErrors(items []diagnostic.Item, id job.NodeID) bool {
	for _, item := range items {
		if item.Severity == diagnostic.ErrorSeverity && item.Path.Component == id.String() {
			return true
		}
	}
	return false
}
