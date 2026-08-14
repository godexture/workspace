package host

import (
	"context"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/plugin"
)

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
	// A release that fails here has not stopped useful work -- the data path is
	// already complete -- so it is a cleanup failure, and it is recorded before
	// the task report decides whether the run has a primary failure at all.
	discarded := invoke(r.ctx, ClosePhase, "", "runtime/discard", func(context.Context) error {
		return execution.Discard()
	})
	if discarded != nil {
		r.addCleanup(*discarded)
	}
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
