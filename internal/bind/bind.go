package bind

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
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
	node    job.Node
	port    string
	entry   bound.Entry
	carrier bool
}

func Normalize(registry Registry, request job.Job) (Result, error) {
	if !request.Valid() || !registry.Valid() {
		return Result{}, diagnostic.NewError(bindItem("bind.invalid-request", plugin.Identity{}, "Job boundary request is invalid", nil))
	}
	inputs := request.Inputs()
	outputs := request.Outputs()
	if len(inputs) > 1 || len(outputs) > 1 {
		return Result{}, diagnostic.NewError(bindItem("bind.mapping-required", plugin.Identity{}, "multiple Job boundaries require explicit stream mapping", map[string]string{
			"inputs": strconv.Itoa(len(inputs)), "outputs": strconv.Itoa(len(outputs)),
		}))
	}

	graph, hasGraph := request.Graph()
	nodes := graph.Nodes()
	edges := graph.Edges()
	mappings := graph.Mappings()
	var openInputs []openInput
	var openOutputs []job.Port
	if hasGraph {
		var err error
		openInputs, openOutputs, err = registry.openPorts(nodes, edges)
		if err != nil {
			return Result{}, err
		}
		if len(inputs) != len(openInputs) {
			return Result{}, boundaryCountError(plan.InputBoundary, len(inputs), len(openInputs))
		}
		if len(outputs) != len(openOutputs) {
			return Result{}, boundaryCountError(plan.OutputBoundary, len(outputs), len(openOutputs))
		}
	}
	used := make(map[job.NodeID]struct{}, len(nodes)+len(inputs)+len(outputs))
	for _, node := range nodes {
		used[node.ID()] = struct{}{}
	}

	var inputSelections, outputSelections []selected
	var entries []bound.Entry
	for index, input := range inputs {
		selection, err := registry.bindInput(input, index, used, openInputFor(hasGraph, openInputs, index))
		if err != nil {
			return Result{}, err
		}
		if selection.carrier {
			used[selection.node.ID()] = struct{}{}
			nodes = append(nodes, selection.node)
		}
		inputSelections = append(inputSelections, selection)
		entries = append(entries, selection.entry)
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
		for index, selection := range inputSelections {
			if selection.carrier {
				edges = append(edges, job.Connect(job.At(selection.node.ID(), selection.port), openInputs[index].port))
			}
		}
		for index, selection := range outputSelections {
			edges = append(edges, job.Connect(openOutputs[index], job.At(selection.node.ID(), selection.port)))
		}
	}
	if err := registry.validatePinnedFormats(inputs, outputs, nodes, edges, entries); err != nil {
		return Result{}, err
	}
	selectedEntries, err := registry.selectCapabilities(nodes, edges, entries, request.Policy().Resources)
	if err != nil {
		return Result{}, err
	}
	entries = selectedEntries

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

func (r Registry) bindInput(input job.Input, index int, used map[job.NodeID]struct{}, target openInput) (selected, error) {
	if target.direct {
		if input.Kind() != job.ReferenceInput {
			return selected{}, directAnchorInputUnsupported(input.Kind(), target.port)
		}
		reference, _ := input.Reference()
		provider, ok := r.sources[reference.Scheme()]
		if !ok {
			return selected{}, missingProvider(reference.Scheme(), plan.InputBoundary)
		}
		return r.anchoredSourceSelection(provider, reference, index, target.port)
	}
	switch input.Kind() {
	case job.ReferenceInput:
		reference, _ := input.Reference()
		provider, ok := r.sources[reference.Scheme()]
		if !ok {
			return selected{}, missingProvider(reference.Scheme(), plan.InputBoundary)
		}
		return r.sourceSelection(provider, reference, index, used)
	case job.EndpointInput:
		request, _ := input.Endpoint()
		return r.endpointSelection(request, index, plan.InputBoundary, used)
	case job.SourceInput:
		direct, _ := input.Direct()
		return r.directSelection(direct, index, plan.InputBoundary, used)
	default:
		return selected{}, diagnostic.NewError(bindItem("bind.input-kind", plugin.Identity{}, "Job input kind is invalid", nil))
	}
}

func (r Registry) bindOutput(output job.Output, index int, used map[job.NodeID]struct{}) (selected, error) {
	switch output.Kind() {
	case job.ReferenceOutput:
		reference, _ := output.Reference()
		provider, ok := r.sinks[reference.Scheme()]
		if !ok {
			return selected{}, missingProvider(reference.Scheme(), plan.OutputBoundary)
		}
		return r.sinkSelection(provider, reference, index, used)
	case job.EndpointOutput:
		request, _ := output.Endpoint()
		return r.endpointSelection(request, index, plan.OutputBoundary, used)
	case job.SinkOutput:
		direct, _ := output.Direct()
		return r.directSelection(direct, index, plan.OutputBoundary, used)
	default:
		return selected{}, diagnostic.NewError(bindItem("bind.output-kind", plugin.Identity{}, "Job output kind is invalid", nil))
	}
}

func (r Registry) directSelection(direct job.Direct, index int, direction plan.BoundaryDirection, used map[job.NodeID]struct{}) (selected, error) {
	if !direct.Valid() {
		return selected{}, diagnostic.NewError(bindItem("bind.direct-resource", plugin.Identity{}, "direct resource binding is invalid", nil))
	}
	adaptor := direct.Adaptor()
	component, ok := r.index.Lookup(adaptor.Component())
	if !ok {
		return selected{}, diagnostic.NewError(bindItem("bind.direct-adaptor", adaptor.Component(), "direct resource adaptor is not in the Host catalog", nil))
	}
	port, err := boundaryPort(component, adaptor.Config(), direction)
	if err != nil {
		return selected{}, err
	}
	id := boundaryNodeID(direction, index, adaptor.Component(), used)
	projection := plan.Boundary{
		Direction: direction,
		Kind:      plan.DirectBoundary,
		Choice:    index,
		Node:      id.String(),
		Port:      port,
		Component: adaptor.Component().String(),
		Ownership: direct.Ownership(),
	}
	return selected{
		node:    job.NewNode(id, adaptor.Component(), adaptor.Config()),
		port:    port,
		entry:   bound.Direct(projection, direct.Opening(), direct.Close),
		carrier: true,
	}, nil
}

func (r Registry) sourceSelection(provider sourceBinding, reference access.Reference, index int, used map[job.NodeID]struct{}) (selected, error) {
	component, _ := r.index.Lookup(provider.component)
	patch := config.NewPatch()
	port, err := boundaryPort(component, patch, plan.InputBoundary)
	if err != nil {
		return selected{}, err
	}
	id := boundaryNodeID(plan.InputBoundary, index, provider.component, used)
	projection := plan.Boundary{
		Direction:            plan.InputBoundary,
		Kind:                 plan.ProviderBoundary,
		Choice:               index,
		Node:                 id.String(),
		Port:                 port,
		Component:            provider.component.String(),
		Scheme:               reference.Scheme(),
		Reference:            reference.Display(),
		ReferenceFingerprint: reference.Fingerprint().String(),
		Available:            provider.trait.Capabilities().Values(),
		Effective:            provider.trait.Capabilities().Values(),
	}
	return selected{node: job.NewNode(id, provider.component, patch), port: port, entry: bound.Source(projection, reference, provider.trait), carrier: true}, nil
}

func (r Registry) anchoredSourceSelection(provider sourceBinding, reference access.Reference, index int, anchor job.Port) (selected, error) {
	if !anchor.Valid() {
		return selected{}, diagnostic.NewError(bindItem("bind.format-direct-anchor", provider.component, "direct Format reader anchor is invalid", nil))
	}
	projection := plan.Boundary{
		Direction:            plan.InputBoundary,
		Kind:                 plan.ProviderBoundary,
		Choice:               index,
		Node:                 anchor.Node().String(),
		Port:                 anchor.ID(),
		Component:            provider.component.String(),
		Scheme:               reference.Scheme(),
		Reference:            reference.Display(),
		ReferenceFingerprint: reference.Fingerprint().String(),
		Available:            provider.trait.Capabilities().Values(),
		Effective:            provider.trait.Capabilities().Values(),
	}
	return selected{port: anchor.ID(), entry: bound.AnchoredSource(projection, reference, provider.trait, anchor.Node())}, nil
}

func (r Registry) sinkSelection(provider sinkBinding, reference access.Reference, index int, used map[job.NodeID]struct{}) (selected, error) {
	component, _ := r.index.Lookup(provider.component)
	patch := config.NewPatch()
	port, err := boundaryPort(component, patch, plan.OutputBoundary)
	if err != nil {
		return selected{}, err
	}
	id := boundaryNodeID(plan.OutputBoundary, index, provider.component, used)
	projection := plan.Boundary{
		Direction:            plan.OutputBoundary,
		Kind:                 plan.ProviderBoundary,
		Choice:               index,
		Node:                 id.String(),
		Port:                 port,
		Component:            provider.component.String(),
		Scheme:               reference.Scheme(),
		Reference:            reference.Display(),
		ReferenceFingerprint: reference.Fingerprint().String(),
		Available:            provider.trait.Capabilities().Values(),
		Effective:            provider.trait.Capabilities().Values(),
	}
	return selected{node: job.NewNode(id, provider.component, patch), port: port, entry: bound.Sink(projection, reference, provider.trait), carrier: true}, nil
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
	return selected{node: job.NewNode(id, request.Component(), request.Config()), port: port, entry: bound.Endpoint(projection, trait), carrier: true}, nil
}

func boundaryPort(component plugin.Component, patch config.Patch, direction plan.BoundaryDirection) (string, error) {
	_, err := component.Resolve(patch)
	if err != nil {
		return "", err
	}
	shape := component.Ports()
	port, valid := bound.Port(shape, direction)
	if !valid {
		return "", diagnostic.NewError(bindItem("bind.endpoint-shape", component.Identity(), "boundary component must have exactly one directional port", map[string]string{"direction": strconv.Itoa(int(direction))}))
	}
	return port.ID(), nil
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
	for nonce := 0; ; nonce++ {
		digest := sha256.Sum256([]byte("godec/bind/node/v1\x00" + base + "\x00" + identity.String() + "\x00" + strconv.Itoa(nonce)))
		candidate := job.NodeID(base + "-" + hex.EncodeToString(digest[:8]))
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

func openInputFor(hasGraph bool, values []openInput, index int) openInput {
	if !hasGraph || index < 0 || index >= len(values) {
		return openInput{}
	}
	return values[index]
}

func directAnchorInputUnsupported(kind job.InputKind, anchor job.Port) error {
	return diagnostic.NewError(bindItem("bind.format-direct-input", plugin.Identity{}, "a direct Format reader accepts only ReferenceInput in M7", map[string]string{
		"kind":      strconv.Itoa(int(kind)),
		"node":      anchor.Node().String(),
		"port":      anchor.ID(),
		"milestone": "M7",
	}))
}

func missingProvider(scheme string, direction plan.BoundaryDirection) error {
	return diagnostic.NewError(bindItem("bind.provider-not-found", plugin.Identity{}, "no Access Provider trait matches the reference scheme and direction", map[string]string{
		"scheme": scheme, "direction": strconv.Itoa(int(direction)),
	}))
}

func boundaryCountError(direction plan.BoundaryDirection, choices, ports int) error {
	return diagnostic.NewError(bindItem("bind.ambiguous-boundary", plugin.Identity{}, "Job choices do not match the explicit graph's open boundary ports", map[string]string{
		"direction": strconv.Itoa(int(direction)), "choices": strconv.Itoa(choices), "ports": strconv.Itoa(ports),
	}))
}
