package host

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/endpoint"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/observe"
	runtimeflow "github.com/godexture/godec/internal/run"
	"github.com/godexture/godec/internal/task"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type outputRuntime struct {
	node            int
	outcome         int
	class           access.TransactionClass
	transaction     access.Transaction
	flusher         access.Flusher
	syncer          access.Syncer
	opened          bool
	prepared        bool
	commitAttempted bool
	committed       bool
}

type runner struct {
	prepared *Prepared
	ctx      context.Context
	cancel   context.CancelCauseFunc
	observe  *observe.Collector
	diag     *diagnosticLog
	plugins  *task.Group
	data     *task.Group

	operators []flow.Operator
	nodes     []graph.Node
	opened    []int
	boundary  map[string]bound.Entry
	outputs   []*outputRuntime
	byOutput  map[int]*outputRuntime
	execution *runtimeflow.Execution
	result    Result
	reported  map[string]struct{}
}

// Run opens and executes a Prepared job exactly once. All operators and
// reservations are released before it returns, including on cancellation.
func (p *Prepared) Run(ctx context.Context) (Result, error) {
	if p == nil {
		failure := Failure{Phase: RunPhase, Err: errors.New("prepared job is nil")}
		return Result{Primary: &failure}, &failure
	}
	if ctx == nil {
		ctx = context.Background()
	}
	jobContext, cancel := context.WithCancelCause(ctx)
	p.mu.Lock()
	if p.state != preparedReady {
		p.mu.Unlock()
		cancel(context.Canceled)
		failure := Failure{Phase: RunPhase, Err: errors.New("prepared job can only run once")}
		return Result{Primary: &failure}, &failure
	}
	p.state = preparedRunning
	p.cancel = cancel
	p.mu.Unlock()

	r := newRunner(p, jobContext, cancel)
	r.execute()
	for _, failure := range p.releaseReservations() {
		r.addCleanup(failure)
	}
	r.finishSnapshots()
	err := resultError(r.result)
	p.complete(err)
	return r.result, err
}

func newRunner(prepared *Prepared, ctx context.Context, cancel context.CancelCauseFunc) *runner {
	collector := observe.New(observe.Mode(prepared.observation), nil)
	nodes := prepared.program.Nodes()
	r := &runner{
		prepared:  prepared,
		ctx:       ctx,
		cancel:    cancel,
		observe:   collector,
		diag:      &diagnosticLog{},
		nodes:     nodes,
		operators: make([]flow.Operator, len(nodes)),
		boundary:  make(map[string]bound.Entry),
		byOutput:  make(map[int]*outputRuntime),
		reported:  make(map[string]struct{}),
	}
	r.plugins = task.NewLinked(ctx, cancel)
	r.data = task.NewLinked(ctx, cancel)
	for _, entry := range prepared.program.Boundaries().Entries() {
		r.boundary[entry.Projection().Node] = entry
	}
	r.initializeOutputs()
	return r
}

func (r *runner) execute() {
	if err := context.Cause(r.ctx); err != nil {
		r.setPrimary(failureOf(RunPhase, "", "", err))
		r.cleanup()
		return
	}
	if failure := r.open(); failure != nil {
		r.setPrimary(*failure)
		r.cleanup()
		return
	}
	if failure := r.runData(); failure != nil {
		r.setPrimary(*failure)
		r.cleanup()
		return
	}
	if failure := r.finishOutputs(); failure != nil {
		r.setPrimary(*failure)
		r.cleanup()
		return
	}
	r.cleanup()
}

func (r *runner) initializeOutputs() {
	for index, node := range r.nodes {
		if len(node.Shape().Outputs) != 0 {
			continue
		}
		class := access.TransactionClass(0)
		if entry, ok := r.boundary[node.ID().String()]; ok && entry.Projection().Kind == plan.ProviderBoundary {
			class = entry.Provider().TransactionClass()
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
	for index, node := range r.nodes {
		entry, ok := r.boundary[node.ID().String()]
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
		direction := access.SourceDirection
		if projection.Direction == plan.OutputBoundary {
			direction = access.SinkDirection
		}
		return access.NewOpening(direction, entry.Reference(), entry.Provider().Capabilities(), projection.Selected, entry.Provider().TransactionClass())
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

func (r *runner) runData() *Failure {
	execution, err := r.prepared.program.BuildObserved(r.operators, r.observe)
	if err != nil {
		failure := failureOf(OpenPhase, "", "runtime/build", err)
		return &failure
	}
	r.execution = execution
	if err := execution.Start(r.data); err != nil {
		failure := failureOf(RunPhase, "", "runtime/start", err)
		return &failure
	}
	if err := execution.WaitSources(r.ctx, r.data); err != nil {
		failure := failureOf(RunPhase, "", "runtime/source", err)
		return &failure
	}
	if failure := r.invoke(RunPhase, "", "runtime/quiesce", execution.Quiesce); failure != nil {
		return failure
	}
	if failure := r.finalize(); failure != nil {
		return failure
	}
	if failure := r.invoke(FlushPhase, "", "runtime/finish", func(context.Context) error {
		return execution.Finish(r.ctx)
	}); failure != nil {
		return failure
	}
	report := r.data.Wait(r.ctx)
	execution.Discard()
	if failure := r.acceptTaskReport(report, false); failure != nil {
		return failure
	}
	if err := context.Cause(r.ctx); err != nil {
		failure := failureOf(RunPhase, "", "", err)
		return &failure
	}
	return nil
}

func (r *runner) finalize() *Failure {
	for index, node := range r.nodes {
		if node.Compilation().Finalization() != plugin.RequiresFinalization {
			continue
		}
		finalizer := r.operators[index].(flow.Finalizer)
		r.emitLifecycle(node.ID().String(), FinalizePhase, "start")
		if failure := r.invoke(FinalizePhase, node.ID().String(), "", finalizer.Finalize); failure != nil {
			return failure
		}
		r.emitLifecycle(node.ID().String(), FinalizePhase, "complete")
	}
	return nil
}

func (r *runner) finishOutputs() *Failure {
	for _, output := range r.outputs {
		if output.flusher == nil {
			continue
		}
		node := r.result.Outputs[output.outcome].Node
		if failure := r.invoke(FlushPhase, node, "", output.flusher.Flush); failure != nil {
			return failure
		}
	}
	for _, output := range r.outputs {
		if output.syncer == nil {
			continue
		}
		node := r.result.Outputs[output.outcome].Node
		if failure := r.invoke(SyncPhase, node, "", output.syncer.Sync); failure != nil {
			return failure
		}
	}
	for _, output := range r.outputs {
		if output.transaction == nil {
			continue
		}
		node := r.result.Outputs[output.outcome].Node
		if failure := r.invoke(PrepareCommitPhase, node, "", output.transaction.PrepareCommit); failure != nil {
			return failure
		}
		output.prepared = true
	}
	for _, output := range r.outputs {
		if output.transaction == nil {
			output.committed = true
			r.result.Outputs[output.outcome].State = OutputCommitted
			continue
		}
		node := r.result.Outputs[output.outcome].Node
		output.commitAttempted = true
		if failure := r.invoke(CommitPhase, node, "", output.transaction.Commit); failure != nil {
			r.result.Outputs[output.outcome].State = OutputUnknown
			return failure
		}
		output.committed = true
		r.result.Outputs[output.outcome].State = OutputCommitted
	}
	return nil
}

func (r *runner) invoke(phase Phase, node, taskName string, work func(context.Context) error) *Failure {
	if err := context.Cause(r.ctx); err != nil {
		failure := failureOf(phase, node, taskName, err)
		return &failure
	}
	return invoke(r.ctx, phase, node, taskName, work)
}

func invoke(ctx context.Context, phase Phase, node, taskName string, work func(context.Context) error) (failure *Failure) {
	defer func() {
		if recovered := recover(); recovered != nil {
			value := failureOf(phase, node, taskName, fmt.Errorf("panic: %v", recovered))
			value.Stack = append([]byte(nil), debug.Stack()...)
			failure = &value
		}
	}()
	if err := work(ctx); err != nil {
		value := failureOf(phase, node, taskName, err)
		return &value
	}
	return nil
}

func failureOf(phase Phase, node, taskName string, err error) Failure {
	var existing *Failure
	if errors.As(err, &existing) && existing != nil {
		value := *existing
		value.Stack = append([]byte(nil), existing.Stack...)
		return value
	}
	var panicError *task.PanicError
	if errors.As(err, &panicError) {
		if node == "" {
			node = panicError.Location
		}
		if taskName == "" {
			taskName = panicError.Name
		}
		return Failure{Phase: phase, Node: node, Task: taskName, Err: err, Stack: append([]byte(nil), panicError.Stack...)}
	}
	failure := Failure{Phase: phase, Node: node, Task: taskName, Err: err}
	for _, item := range diagnostic.ItemsOf(err) {
		if stack := item.Detail["stack"]; stack != "" {
			failure.Stack = []byte(stack)
			break
		}
	}
	return failure
}

func resultError(result Result) error {
	values := make([]error, 0, 1+len(result.Cleanup))
	if result.Primary != nil {
		values = append(values, *result.Primary)
	}
	for _, failure := range result.Cleanup {
		values = append(values, failure)
	}
	return errors.Join(values...)
}

func (r *runner) acceptTaskReport(report task.Report, cleanup bool) *Failure {
	failures := append([]task.Failure(nil), report.Failures...)
	sort.SliceStable(failures, func(left, right int) bool { return failures[left].Name < failures[right].Name })
	var primary *Failure
	for _, value := range failures {
		key := "failure:" + value.Name + ":" + value.Err.Error()
		if _, exists := r.reported[key]; exists {
			continue
		}
		r.reported[key] = struct{}{}
		failure := failureOf(RunPhase, "", value.Name, value.Err)
		if cleanup {
			r.addCleanup(failure)
		} else if primary == nil {
			copy := failure
			primary = &copy
		} else {
			r.addSecondary(failure)
		}
	}
	for _, name := range report.Running {
		key := "running:" + name
		if _, exists := r.reported[key]; exists {
			continue
		}
		r.reported[key] = struct{}{}
		failure := failureOf(JoinPhase, "", name, errors.New("task is still running after the cleanup bound"))
		if cleanup {
			r.addCleanup(failure)
		} else if primary == nil {
			copy := failure
			primary = &copy
		} else {
			r.addSecondary(failure)
		}
	}
	if report.WaitErr != nil {
		key := "wait:" + report.WaitErr.Error()
		if _, exists := r.reported[key]; !exists {
			r.reported[key] = struct{}{}
			failure := failureOf(JoinPhase, "", "", report.WaitErr)
			if cleanup {
				r.addCleanup(failure)
			} else if primary == nil {
				copy := failure
				primary = &copy
			} else {
				r.addSecondary(failure)
			}
		}
	}
	return primary
}
