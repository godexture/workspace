package host

import (
	"context"
	"errors"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/observe"
	runtimeflow "github.com/godexture/godec/internal/run"
	"github.com/godexture/godec/internal/task"
)

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
func (p *Prepared) Run(ctx context.Context, options ...RunOption) (Result, error) {
	configuration, err := resolveRunOptions(options)
	if err != nil {
		failure := Failure{Phase: RunPhase, Err: err}
		return Result{Primary: &failure}, &failure
	}
	return p.run(ctx, configuration)
}

func (p *Prepared) run(ctx context.Context, configuration runOptions) (Result, error) {
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

	r := newRunner(p, jobContext, cancel, configuration, ctx)
	r.execute()
	r.finishObservation()
	r.finishSnapshots()
	err := resultError(r.result)
	p.complete(err)
	return r.result, err
}

func newRunner(prepared *Prepared, ctx context.Context, cancel context.CancelCauseFunc, options runOptions, observationContext context.Context) *runner {
	collector := newObservationCollector(options, observationContext, cancel)
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
