package host

import (
	"context"
	"errors"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/cancel"
	"github.com/godexture/godec/internal/graph"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/observe"
	runtimeflow "github.com/godexture/godec/internal/run"
	"github.com/godexture/godec/internal/task"
	"github.com/godexture/godec/plan"
)

type runner struct {
	prepared    *Prepared
	ctx         context.Context
	cancel      context.CancelCauseFunc
	phase       context.Context
	phaseCancel context.CancelCauseFunc
	observe     *observe.Collector
	diag        *diagnosticLog
	ledger      *journal.Ledger
	plugins     *task.Group
	data        *task.Group

	operators      []flow.Operator
	nodes          []graph.Node
	opened         []int
	owners         []*journal.Domain
	boundary       map[string]bound.Entry
	outputs        []*outputRuntime
	byOutput       map[int]*outputRuntime
	metadataLosses []plan.PredictedMetadataLoss
	execution      *runtimeflow.Execution
	result         Result
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
	phaseContext, phaseCancel, detach := cancel.Link(ctx)
	defer detach()
	jobContext, jobCancel := context.WithCancelCause(phaseContext)
	stop := func(cause error) {
		if cause == nil {
			cause = context.Canceled
		}
		phaseCancel(cause)
		jobCancel(cause)
	}
	p.mu.Lock()
	if p.state != preparedReady {
		p.mu.Unlock()
		phaseCancel(context.Canceled)
		jobCancel(context.Canceled)
		failure := Failure{Phase: RunPhase, Err: errors.New("prepared job can only run once")}
		return Result{Primary: &failure}, &failure
	}
	p.state = preparedRunning
	p.stop = stop
	p.mu.Unlock()

	r := newRunner(p, jobContext, jobCancel, phaseContext, phaseCancel, configuration, ctx)
	r.execute()
	r.finishObservation()
	r.ledger.RecordOwnershipFailures()
	// Everything has joined, closed, and released by now, so nothing can still
	// be writing to the ledger. Collecting it here, once, is what makes each
	// failure appear in the Result exactly once.
	r.collect()
	r.finishSnapshots()
	phaseCancel(context.Canceled)
	err := resultError(r.result)
	p.complete(err)
	return r.result, err
}

func newRunner(prepared *Prepared, ctx context.Context, cancel context.CancelCauseFunc, phase context.Context, phaseCancel context.CancelCauseFunc, options runOptions, observationContext context.Context) *runner {
	nodes := prepared.program.Nodes()
	ledger := journal.NewLedger()
	if options.verifyOwnership {
		ledger.EnableOwnershipAudit()
	}
	r := &runner{
		prepared:       prepared,
		ctx:            ctx,
		cancel:         cancel,
		phase:          phase,
		phaseCancel:    phaseCancel,
		diag:           &diagnosticLog{},
		ledger:         ledger,
		nodes:          nodes,
		operators:      make([]flow.Operator, len(nodes)),
		owners:         make([]*journal.Domain, len(nodes)),
		boundary:       make(map[string]bound.Entry),
		byOutput:       make(map[int]*outputRuntime),
		metadataLosses: prepared.program.Plan().PredictedMetadataLosses(),
	}
	r.observe = r.newObservationCollector(options, observationContext)
	fail := func(err error) {
		r.stop(err)
	}
	r.plugins = task.NewLinked(ctx, ledger, fail)
	r.data = task.NewLinked(ctx, ledger, fail)
	for _, entry := range prepared.program.Boundaries().Entries() {
		r.boundary[entry.Projection().Node] = entry
	}
	r.initializeOutputs()
	return r
}

// stop cancels the job for every cause and cancels the lifecycle phase unless
// the cause came from Flush. Flush failures are peer failures: keeping the
// phase context alive lets the remaining independent Flush operations make
// their own attempt and record their own evidence.
func (r *runner) stop(cause error) {
	if r == nil {
		return
	}
	if cause == nil {
		cause = context.Canceled
	}
	if r.phaseCancel != nil && journal.OperationOf(cause) != journal.Flush {
		r.phaseCancel(cause)
	}
	if r.cancel != nil {
		r.cancel(cause)
	}
}

// execute walks the run's lifecycle. Every step that fails has already put its
// failure in the ledger by the time it returns one, so the only thing left to
// decide here is whether to keep going.
func (r *runner) execute() {
	defer r.cleanup()
	if err := context.Cause(r.ctx); err != nil {
		r.record(journal.WorkError, journal.Run, "", "", err)
		return
	}
	// The Plan describes the bytes Probe and Inspect saw. Confirm the sources
	// still hold them before opening operators, and again before an output is
	// committed, so a source that changed under the job cannot produce a
	// successful conversion of content nobody planned for.
	if failure := verifySnapshots(r.ctx, RunPhase, r.prepared.sessions); failure != nil {
		r.adopt(journal.WorkError, *failure)
		return
	}
	if r.open() != nil {
		return
	}
	if r.runData() != nil {
		return
	}
	if failure := verifySnapshots(r.ctx, CommitPhase, r.prepared.sessions); failure != nil {
		r.adopt(journal.WorkError, *failure)
		return
	}
	r.finishOutputs()
}
