package bind

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type Result struct {
	request    job.Job
	boundaries bound.State
}

func (r Result) Request() job.Job        { return r.request }
func (r Result) Boundaries() bound.State { return bound.New(r.boundaries.Entries()...) }
func (r Result) Valid() bool             { return r.request.Valid() && r.boundaries.Valid() }

type selected struct {
	node  job.Node
	port  string
	entry bound.Entry
}

func Normalize(registry Registry, request job.Job) (Result, error) {
	if !request.Valid() || !registry.Valid() {
		return Result{}, diagnostic.NewError(bindItem("bind.invalid-request", plugin.Identity{}, "Job boundary request is invalid", nil))
	}
	inputs := request.Inputs()
	outputs := request.Outputs()
	if len(inputs) == 0 && len(outputs) == 0 {
		return Result{request: request, boundaries: bound.State{}}, nil
	}
	if len(inputs) > 1 || len(outputs) > 1 {
		return Result{}, diagnostic.NewError(bindItem("bind.mapping-required", plugin.Identity{}, "multiple Job boundaries require explicit stream mapping", map[string]string{
			"inputs": strconv.Itoa(len(inputs)), "outputs": strconv.Itoa(len(outputs)),
		}))
	}

	graph, hasGraph := request.Graph()
	nodes := graph.Nodes()
	edges := graph.Edges()
	mappings := graph.Mappings()
	used := make(map[job.NodeID]struct{}, len(nodes)+len(inputs)+len(outputs))
	for _, node := range nodes {
		used[node.ID()] = struct{}{}
	}

	var inputSelections, outputSelections []selected
	var entries []bound.Entry
	for index, input := range inputs {
		selection, err := registry.bindInput(input, index, used)
		if err != nil {
			return Result{}, err
		}
		used[selection.node.ID()] = struct{}{}
		inputSelections = append(inputSelections, selection)
		entries = append(entries, selection.entry)
		nodes = append(nodes, selection.node)
	}
	for index, output := range outputs {
		selection, err := registry.bindOutput(output, index, used)
		if err != nil {
			return Result{}, err
		}
		used[selection.node.ID()] = struct{}{}
		outputSelections = append(outputSelections, selection)
		entries = append(entries, selection.entry)
		nodes = append(nodes, selection.node)
	}

	if !hasGraph {
		if len(inputSelections) != 1 || len(outputSelections) != 1 {
			return Result{}, diagnostic.NewError(bindItem("bind.incomplete-boundary", plugin.Identity{}, "a Job without an explicit graph needs one input and one output", nil))
		}
		edges = append(edges, job.Connect(
			job.At(inputSelections[0].node.ID(), inputSelections[0].port),
			job.At(outputSelections[0].node.ID(), outputSelections[0].port),
		))
	} else {
		originalNodes := graph.Nodes()
		openInputs, openOutputs, err := registry.openPorts(originalNodes, edges)
		if err != nil {
			return Result{}, err
		}
		if len(inputSelections) != len(openInputs) {
			return Result{}, boundaryCountError(plan.InputBoundary, len(inputSelections), len(openInputs))
		}
		if len(outputSelections) != len(openOutputs) {
			return Result{}, boundaryCountError(plan.OutputBoundary, len(outputSelections), len(openOutputs))
		}
		for index, selection := range inputSelections {
			edges = append(edges, job.Connect(job.At(selection.node.ID(), selection.port), openInputs[index]))
		}
		for index, selection := range outputSelections {
			edges = append(edges, job.Connect(openOutputs[index], job.At(selection.node.ID(), selection.port)))
		}
	}

	normalizedGraph, err := job.NewGraph(nodes, edges, mappings...)
	if err != nil {
		return Result{}, err
	}
	normalized, err := job.New(inputs, outputs, normalizedGraph, job.WithPolicy(request.Policy()), job.WithBudget(request.Budget()))
	if err != nil {
		return Result{}, err
	}
	state := bound.New(entries...)
	if !state.Valid() {
		return Result{}, diagnostic.NewError(bindItem("bind.invalid-state", plugin.Identity{}, "normalized boundary state is invalid", nil))
	}
	return Result{request: normalized, boundaries: state}, nil
}

func (r Registry) bindInput(input job.Input, index int, used map[job.NodeID]struct{}) (selected, error) {
	switch input.Kind() {
	case job.ReferenceInput:
		reference, _ := input.Reference()
		provider, ok := r.providers[reference.Scheme()]
		if !ok {
			return selected{}, missingProvider(reference.Scheme())
		}
		if !provider.Role().AllowsSource() {
			return selected{}, directionError(provider.Identity(), plan.InputBoundary)
		}
		selection, err := selectCapabilities(provider)
		if err != nil {
			return selected{}, err
		}
		return r.providerSelection(provider, reference, selection, index, plan.InputBoundary, used)
	case job.EndpointInput:
		request, _ := input.Endpoint()
		return r.endpointSelection(request, index, plan.InputBoundary, used)
	case job.SourceInput:
		return selected{}, diagnostic.NewError(bindItem("bind.direct-resource", plugin.Identity{}, "direct Source adaptors are built by Prepare in M5", map[string]string{"direction": "input"}))
	default:
		return selected{}, diagnostic.NewError(bindItem("bind.input-kind", plugin.Identity{}, "Job input kind is invalid", nil))
	}
}

func (r Registry) bindOutput(output job.Output, index int, used map[job.NodeID]struct{}) (selected, error) {
	switch output.Kind() {
	case job.ReferenceOutput:
		reference, _ := output.Reference()
		provider, ok := r.providers[reference.Scheme()]
		if !ok {
			return selected{}, missingProvider(reference.Scheme())
		}
		if !provider.Role().AllowsSink() {
			return selected{}, directionError(provider.Identity(), plan.OutputBoundary)
		}
		return r.providerSelection(provider, reference, access.Selection{}, index, plan.OutputBoundary, used)
	case job.EndpointOutput:
		request, _ := output.Endpoint()
		return r.endpointSelection(request, index, plan.OutputBoundary, used)
	case job.SinkOutput:
		return selected{}, diagnostic.NewError(bindItem("bind.direct-resource", plugin.Identity{}, "direct Sink adaptors are built by Prepare in M5", map[string]string{"direction": "output"}))
	default:
		return selected{}, diagnostic.NewError(bindItem("bind.output-kind", plugin.Identity{}, "Job output kind is invalid", nil))
	}
}

func (r Registry) providerSelection(provider access.Provider, reference access.Reference, capability access.Selection, index int, direction plan.BoundaryDirection, used map[job.NodeID]struct{}) (selected, error) {
	component, _ := r.index.Lookup(provider.Identity())
	patch := config.NewPatch()
	port, err := boundaryPort(component, patch, direction)
	if err != nil {
		return selected{}, err
	}
	id := boundaryNodeID(direction, index, provider.Identity(), used)
	projection := plan.Boundary{
		Direction:            direction,
		Kind:                 plan.ProviderBoundary,
		Choice:               index,
		Node:                 id.String(),
		Port:                 port,
		Component:            provider.Identity().String(),
		Scheme:               reference.Scheme(),
		Reference:            reference.Display(),
		ReferenceFingerprint: reference.Fingerprint().String(),
		Available:            provider.Capabilities().Values(),
		Selected:             capability.Capabilities(),
	}
	return selected{node: job.NewNode(id, provider.Identity(), patch), port: port, entry: bound.Provider(projection, reference, provider)}, nil
}

func (r Registry) endpointSelection(request job.EndpointRequest, index int, direction plan.BoundaryDirection, used map[job.NodeID]struct{}) (selected, error) {
	trait, ok := r.endpoints[request.Component()]
	if !ok {
		return selected{}, diagnostic.NewError(bindItem("bind.endpoint-not-found", request.Component(), "Endpoint component is not registered with Host", nil))
	}
	component, _ := r.index.Lookup(request.Component())
	port, err := boundaryPort(component, request.Config(), direction)
	if err != nil {
		return selected{}, err
	}
	id := boundaryNodeID(direction, index, request.Component(), used)
	projection := plan.Boundary{
		Direction: direction,
		Kind:      plan.EndpointBoundary,
		Choice:    index,
		Node:      id.String(),
		Port:      port,
		Component: request.Component().String(),
		Topology:  trait.Topology(),
		Mode:      trait.Mode(),
	}
	return selected{node: job.NewNode(id, request.Component(), request.Config()), port: port, entry: bound.Endpoint(projection, trait)}, nil
}

func selectCapabilities(provider access.Provider) (access.Selection, error) {
	requirements := provider.Requirements()
	if requirements.Empty() {
		return access.Selection{}, nil
	}
	selection, ok := access.Select(provider.Capabilities(), requirements)
	if ok {
		return selection, nil
	}
	available := provider.Capabilities().Values()
	values := make([]string, len(available))
	for index, capability := range available {
		values[index] = string(capability)
	}
	return access.Selection{}, diagnostic.NewError(bindItem("bind.capability", provider.Identity(), "Access Provider cannot satisfy the declared source capability alternatives", map[string]string{
		"available": strings.Join(values, ","),
	}))
}

func boundaryPort(component plugin.Component, patch config.Patch, direction plan.BoundaryDirection) (string, error) {
	resolved, err := component.Resolve(patch)
	if err != nil {
		return "", err
	}
	shape, err := component.Shape(plugin.ShapeContext{}, resolved)
	if err != nil {
		return "", err
	}
	var ports []flow.Port
	valid := false
	switch direction {
	case plan.InputBoundary:
		ports = shape.Outputs
		valid = len(shape.Inputs) == 0 && len(shape.Outputs) == 1
	case plan.OutputBoundary:
		ports = shape.Inputs
		valid = len(shape.Inputs) == 1 && len(shape.Outputs) == 0
	}
	if !valid || ports[0].Multiplicity() != flow.One {
		return "", diagnostic.NewError(bindItem("bind.endpoint-shape", component.Identity(), "boundary component must have exactly one directional port", map[string]string{"direction": strconv.Itoa(int(direction))}))
	}
	return ports[0].ID(), nil
}

func (r Registry) openPorts(nodes []job.Node, edges []job.Edge) ([]job.Port, []job.Port, error) {
	incoming := make(map[string]struct{}, len(edges))
	outgoing := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		incoming[edge.To().String()] = struct{}{}
		outgoing[edge.From().String()] = struct{}{}
	}
	var inputs, outputs []job.Port
	for _, node := range nodes {
		component, ok := r.index.Lookup(node.Component())
		if !ok {
			continue
		}
		resolved, err := component.Resolve(node.Config())
		if err != nil {
			return nil, nil, err
		}
		shape, err := component.Shape(plugin.ShapeContext{}, resolved)
		if err != nil {
			return nil, nil, err
		}
		for _, port := range shape.Inputs {
			value := job.At(node.ID(), port.ID())
			if port.Required() {
				if _, connected := incoming[value.String()]; !connected {
					inputs = append(inputs, value)
				}
			}
		}
		for _, port := range shape.Outputs {
			value := job.At(node.ID(), port.ID())
			if port.Required() {
				if _, connected := outgoing[value.String()]; !connected {
					outputs = append(outputs, value)
				}
			}
		}
	}
	sort.Slice(inputs, func(left, right int) bool { return inputs[left].String() < inputs[right].String() })
	sort.Slice(outputs, func(left, right int) bool { return outputs[left].String() < outputs[right].String() })
	return inputs, outputs, nil
}

func boundaryNodeID(direction plan.BoundaryDirection, index int, identity plugin.Identity, used map[job.NodeID]struct{}) job.NodeID {
	prefix := "input"
	if direction == plan.OutputBoundary {
		prefix = "output"
	}
	base := prefix + "-" + strconv.Itoa(index)
	if _, exists := used[job.NodeID(base)]; !exists {
		return job.NodeID(base)
	}
	digest := sha256.Sum256([]byte("godec/bind/node/v1\x00" + base + "\x00" + identity.String()))
	for length := 8; length <= len(digest); length += 4 {
		candidate := job.NodeID(base + "-" + hex.EncodeToString(digest[:length]))
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
	return job.NodeID(base + "-" + hex.EncodeToString(digest[:]))
}

func missingProvider(scheme string) error {
	return diagnostic.NewError(bindItem("bind.provider-not-found", plugin.Identity{}, "no Access Provider is registered for the reference scheme", map[string]string{"scheme": scheme}))
}

func directionError(identity plugin.Identity, direction plan.BoundaryDirection) error {
	return diagnostic.NewError(bindItem("bind.provider-role", identity, "Access Provider does not support the requested direction", map[string]string{"direction": strconv.Itoa(int(direction))}))
}

func boundaryCountError(direction plan.BoundaryDirection, choices, ports int) error {
	return diagnostic.NewError(bindItem("bind.ambiguous-boundary", plugin.Identity{}, "Job choices do not match the explicit graph's open boundary ports", map[string]string{
		"direction": strconv.Itoa(int(direction)), "choices": strconv.Itoa(choices), "ports": strconv.Itoa(ports),
	}))
}
