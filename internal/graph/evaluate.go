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

// Evaluation is either a complete compiled graph or a deterministic list of
// gaps in an otherwise valid requested graph.
type Evaluation struct {
	graph Graph
	gaps  []Gap
}

func (e Evaluation) Graph() (Graph, bool) { return e.graph, e.graph.Valid() }
func (e Evaluation) Gaps() []Gap          { return append([]Gap(nil), e.gaps...) }

func evaluate(index catalog.Index, requested job.Graph, contexts CompileContexts, allowGaps bool, beforeCompile func() error) (Evaluation, error) {
	if !requested.Valid() {
		return Evaluation{}, diagnostic.NewError(diagnostic.NewItem("graph.invalid-request", diagnostic.ErrorSeverity, diagnostic.Path{}, "requested graph is invalid", nil))
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
		if !component.View().HasSpec {
			items = append(items, diagnostic.NewItem("graph.control-plane-component", diagnostic.ErrorSeverity, diagnostic.Path{Component: request.ID().String()}, "control-plane component cannot be used as a graph node", map[string]string{"identity": component.Identity().String()}))
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
		return Evaluation{}, diagnostic.NewError(items...)
	}
	order, topologyItems := validateTopologyMode(shaped, edges, allowGaps)
	if len(topologyItems) != 0 {
		return Evaluation{}, diagnostic.NewError(topologyItems...)
	}

	compiledByID := make(map[job.NodeID]Node, len(shaped))
	var gaps []Gap
	for _, shapedIndex := range order {
		node := shaped[shapedIndex]
		inputs, blocked, inputItems := inputsFor(node, edges, compiledByID, !allowGaps)
		items = append(items, inputItems...)
		if blocked {
			continue
		}
		component := components[node.componentIndex]
		configValue := configs[node.componentIndex]
		compileContext := contexts.For(node.request.ID())
		if allowGaps {
			schemaGaps := descriptorSchemaGaps(node, edges, compiledByID, component, configValue, compileContext, inputs)
			if len(schemaGaps) != 0 {
				gaps = append(gaps, schemaGaps...)
				continue
			}
		}
		if beforeCompile != nil {
			if err := beforeCompile(); err != nil {
				return Evaluation{}, err
			}
		}
		compilation, err := plugin.Compile(component, compileContext, configValue, inputs)
		if err != nil {
			items = append(items, prefixNode(errorItems(err), node.request.ID())...)
			continue
		}
		requirements, ok := plugin.RequirementsOf[stream.Descriptor](compilation)
		if !ok {
			items = append(items, diagnostic.NewItem("graph.requirement-type", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.request.ID().String()}, "component compilation returned incompatible requirements", nil))
			continue
		}
		if allowGaps {
			for _, requirement := range requirements {
				gaps = append(gaps, gapFor(node, edges, compiledByID, component, configValue, compileContext, inputs, requirement.Need(), requirement.Port()))
			}
		} else {
			for _, requirement := range requirements {
				items = append(items, diagnostic.NewItem("graph.requirement", diagnostic.ErrorSeverity, diagnostic.Path{Component: node.request.ID().String(), Descriptor: requirement.Port()}, "component input requirement is not satisfied", map[string]string{"need": requirement.Need().Code()}))
			}
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
			component:   component,
			config:      configValue,
			shape:       node.shape.Clone(),
			inputs:      inputs,
			compilation: compilation,
		}
	}
	if len(items) != 0 {
		return Evaluation{}, diagnostic.NewError(items...)
	}
	if len(gaps) != 0 {
		sortGaps(gaps)
		return Evaluation{gaps: gaps}, nil
	}
	result := make([]Node, 0, len(order))
	for _, shapedIndex := range order {
		id := shaped[shapedIndex].request.ID()
		node, ok := compiledByID[id]
		if !ok {
			item := diagnostic.NewItem("graph.incomplete", diagnostic.ErrorSeverity, diagnostic.Path{Component: id.String()}, "graph evaluation ended without a compiled node or input gap", nil)
			return Evaluation{}, diagnostic.NewError(item)
		}
		result = append(result, node)
	}
	return Evaluation{graph: newGraph(result, edges)}, nil
}

func inputsFor(node shapedNode, edges []job.Edge, compiled map[job.NodeID]Node, reportBlocked bool) (flow.Descriptors[stream.Descriptor], bool, []diagnostic.Item) {
	incoming := incomingEdges(edges, node.request.ID(), "")
	var bindings []flow.PortDescriptor[stream.Descriptor]
	var items []diagnostic.Item
	blocked := false
	for _, edge := range incoming {
		upstream, ok := compiled[edge.From().Node()]
		if !ok {
			blocked = true
			if reportBlocked {
				items = append(items, graphItem("graph.blocked-input", edge.To(), "upstream node did not compile", map[string]string{"source": edge.From().Node().String()}))
			}
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
	return flow.NewDescriptors(bindings...), blocked, items
}

func incomingEdges(edges []job.Edge, node job.NodeID, port string) []job.Edge {
	var result []job.Edge
	for _, edge := range edges {
		if edge.To().Node() == node && (port == "" || edge.To().ID() == port) {
			result = append(result, edge)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].To().ID() != result[right].To().ID() {
			return result[left].To().ID() < result[right].To().ID()
		}
		return result[left].From().String() < result[right].From().String()
	})
	return result
}

func descriptorSchemaGaps(node shapedNode, edges []job.Edge, compiled map[job.NodeID]Node, component plugin.Component, configValue config.ResolvedView, compileContext plugin.CompileContext, inputs flow.Descriptors[stream.Descriptor]) []Gap {
	var gaps []Gap
	for _, port := range node.shape.Inputs {
		values := inputs.At(port.ID())
		mismatch := false
		for _, descriptor := range values {
			mismatch = mismatch || descriptor.Schema() != port.Schema().Identity()
		}
		if !mismatch {
			continue
		}
		var desired stream.Descriptor
		if len(values) != 0 {
			desired, _ = stream.NewDescriptor(values[0].ID(), port.Schema().Identity(), values[0].TimeBase(), values[0].Properties())
			desired = desired.WithMetadata(values[0].Metadata())
		}
		need := plugin.DescriptorNeed("graph.schema-mismatch", desired)
		gaps = append(gaps, gapFor(node, edges, compiled, component, configValue, compileContext, inputs, need, port.ID()))
	}
	return gaps
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
