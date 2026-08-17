package host

import (
	"context"
	"errors"
	"fmt"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/task"
	"github.com/godexture/godec/job"
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

	r.ledger.EnterStage(journal.Open)
	for _, index := range order {
		if err := context.Cause(r.ctx); err != nil {
			return r.record(journal.WorkError, journal.Open, r.nodes[index].ID().String(), "", err)
		}
		if failure := r.openNode(index); failure != nil {
			return failure
		}
	}
	if err := context.Cause(r.ctx); err != nil {
		return r.record(journal.WorkError, journal.Open, "", "", err)
	}
	r.ledger.EnterStage(journal.Run)
	return nil
}

func (r *runner) openNode(index int) *Failure {
	node := r.nodes[index]
	boundary, err := r.opening(node.ID().String())
	if err != nil {
		return r.record(journal.WorkError, journal.Open, node.ID().String(), "", err)
	}
	source, err := r.sourceOpening(node.ID())
	if err != nil {
		return r.record(journal.WorkError, journal.Open, node.ID().String(), "", err)
	}
	lease := r.prepared.byNode[node.ID()]
	// The component's own failure domain is created here and lives for the
	// whole run, so a payload it retains past a call still reports somewhere
	// the run collects from -- during Flush, during Close, or after both.
	owner := r.ledger.Domain("node/"+node.ID().String(), node.ID().String())
	r.owners[index] = owner
	var scratchService plugin.Scratch
	if journal := r.prepared.scratch[node.ID()]; journal != nil {
		scratchService = journal
	}
	services := plugin.OpenServices{
		Buffers:     lease.Buffers(),
		Tasks:       task.NewStarter(r.plugins, lease.Grant().Workers),
		Diagnostics: r.diag.sink(node.ID().String()),
		Owner:       owner.At(node.ID().String()).Reporter(),
		Boundary:    boundary,
		Source:      source,
		Scratch:     scratchService,
	}
	r.emitLifecycle(node.ID().String(), OpenPhase, "start")
	operator, err := r.prepared.program.Open(plugin.NewOpenContext(r.ctx, services), node.ID())
	if err != nil {
		return r.record(journal.WorkError, journal.Open, node.ID().String(), "", err)
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
		return r.record(journal.WorkError, journal.Open, node.ID().String(), "", fmt.Errorf("output declares %s but operator does not implement access.Transaction", output.class))
	}
	return nil
}

func (r *runner) sourceOpening(node job.NodeID) (any, error) {
	source, ok := r.prepared.sources[node]
	if !ok {
		return nil, nil
	}
	session, ok := r.prepared.bySession[source]
	if !ok || !session.opening.Valid() || session.opening.Direction() != access.SourceDirection {
		return nil, errors.New("prepared Format source opening is missing")
	}
	return session.opening, nil
}

func (r *runner) opening(node string) (any, error) {
	entry, ok := r.boundary[node]
	if !ok {
		return nil, nil
	}
	projection := entry.Projection()
	if entry.Anchor().Valid() {
		return nil, nil
	}
	switch projection.Kind {
	case plan.ProviderBoundary:
		session, ok := r.prepared.bySession[node]
		if !ok || !session.opening.Valid() {
			return nil, errors.New("prepared Access session opening is missing")
		}
		return session.opening, nil
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
