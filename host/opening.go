package host

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/internal/task"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func (r *runner) initializeOutputs() {
	for index, node := range r.nodes {
		if len(node.Shape().Outputs) != 0 {
			continue
		}
		class := access.TransactionClass(0)
		if entry, ok := r.boundary[node.ID().String()]; ok && entry.Projection().Kind == plan.ProviderBoundary {
			class = entry.SinkTrait().TransactionClass()
		}
		outcome := len(r.result.Outputs)
		r.result.Outputs = append(r.result.Outputs, OutputOutcome{
			Node:      node.ID().String(),
			Component: node.Component().String(),
			Class:     class,
			State:     OutputPending,
		})
		value := &outputRuntime{node: index, outcome: outcome, class: class}
		r.outputs = append(r.outputs, value)
		r.byOutput[index] = value
	}
}

func (r *runner) open() *Failure {
	order := make([]int, 0, len(r.nodes))
	added := make([]bool, len(r.nodes))
	for index, node := range r.nodes {
		if len(node.Shape().Outputs) == 0 {
			order = append(order, index)
			added[index] = true
		}
	}
	for index := range r.nodes {
		entry, ok := r.boundary[r.nodes[index].ID().String()]
		if !added[index] && ok && entry.Projection().Kind == plan.EndpointBoundary {
			order = append(order, index)
			added[index] = true
		}
	}
	for index := range r.nodes {
		if !added[index] {
			order = append(order, index)
		}
	}

	for _, index := range order {
		if err := context.Cause(r.ctx); err != nil {
			failure := failureOf(OpenPhase, r.nodes[index].ID().String(), "", err)
			return &failure
		}
		if failure := r.openNode(index); failure != nil {
			return failure
		}
	}
	if err := context.Cause(r.ctx); err != nil {
		failure := failureOf(OpenPhase, "", "", err)
		return &failure
	}
	return nil
}

func (r *runner) openNode(index int) *Failure {
	node := r.nodes[index]
	boundary, err := r.opening(node.ID().String())
	if err != nil {
		failure := failureOf(OpenPhase, node.ID().String(), "", err)
		return &failure
	}
	lease := r.prepared.byNode[node.ID()]
	services := plugin.OpenServices{
		Buffers:     lease.Buffers(),
		Tasks:       task.NewStarter(r.plugins, lease.Grant().Workers),
		Diagnostics: r.diag.sink(node.ID().String()),
		Boundary:    boundary,
	}
	r.emitLifecycle(node.ID().String(), OpenPhase, "start")
	operator, err := r.prepared.program.Open(plugin.NewOpenContext(r.ctx, services), node.ID())
	if err != nil {
		failure := failureOf(OpenPhase, node.ID().String(), "", err)
		return &failure
	}
	r.operators[index] = operator
	r.opened = append(r.opened, index)
	r.emitLifecycle(node.ID().String(), OpenPhase, "complete")

	output := r.byOutput[index]
	if output == nil {
		return nil
	}
	output.opened = true
	output.transaction, _ = operator.(access.Transaction)
	output.flusher, _ = operator.(access.Flusher)
	output.syncer, _ = operator.(access.Syncer)
	if output.class != 0 && output.class != access.LiveNoCommit && output.transaction == nil {
		failure := failureOf(OpenPhase, node.ID().String(), "", fmt.Errorf("output declares %s but operator does not implement access.Transaction", output.class))
		return &failure
	}
	return nil
}

func (r *runner) opening(node string) (any, error) {
	entry, ok := r.boundary[node]
	if !ok {
		return nil, nil
	}
	projection := entry.Projection()
	switch projection.Kind {
	case plan.ProviderBoundary:
		if projection.Direction == plan.InputBoundary {
			return access.NewOpening(access.SourceDirection, entry.Reference(), entry.SourceTrait().Capabilities(), projection.Selected, 0)
		}
		trait := entry.SinkTrait()
		return access.NewOpening(access.SinkDirection, entry.Reference(), trait.Capabilities(), projection.Selected, trait.TransactionClass())
	case plan.EndpointBoundary:
		direction := endpoint.SourceDirection
		if projection.Direction == plan.OutputBoundary {
			direction = endpoint.SinkDirection
		}
		return endpoint.NewOpening(direction, entry.EndpointTrait())
	case plan.DirectBoundary:
		return entry.DirectOpening(), nil
	default:
		return nil, errors.New("unsupported prepared boundary")
	}
}
