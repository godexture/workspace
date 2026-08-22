package host

import (
	"context"

	"github.com/godexture/godec/internal/journal"
)

func (r *runner) cleanup() {
	cause := r.ledger.Stopped()
	if cause == nil {
		cause = context.Canceled
	}
	r.stop(cause)
	cleanupContext, cancel := context.WithTimeout(context.Background(), r.prepared.cleanupTimeout)
	defer cancel()

	if r.execution != nil {
		r.cleanupInvoke(cleanupContext, ClosePhase, "", "runtime", func(context.Context) error {
			r.execution.Abort()
			return nil
		})
	}
	r.data.Cancel(cause)
	r.plugins.Cancel(cause)
	r.acceptTaskReport(r.data.Wait(cleanupContext), true)
	if r.phaseCancel != nil {
		// A Flush failure keeps the phase alive until every prepared drain has
		// had its attempt. Once the bounded wait is over, stop any non-
		// cooperative task before the remaining cleanup phases continue.
		r.phaseCancel(cause)
	}
	if r.execution != nil {
		// Each edge discards in its own domain, under its own Discard span, so
		// a payload released here is still attributed to the task that owned
		// it and a declared Drop that panics past every other owner is
		// recovered there rather than here.
		r.ledger.EnterStage(journal.Discard)
		r.cleanupInvoke(cleanupContext, DiscardPhase, "", "runtime/discard", func(context.Context) error {
			r.execution.Discard()
			return nil
		})
	}
	r.ledger.EnterStage(journal.Close)
	r.acceptTaskReport(r.plugins.Wait(cleanupContext), true)
	r.abortOutputs(cleanupContext)
	r.closeOperators(cleanupContext)
	r.ledger.EnterStage(journal.Resource)
	for _, failure := range r.prepared.releaseScratch(cleanupContext) {
		r.adopt(journal.CleanupError, failure)
	}
	for _, failure := range r.prepared.releaseResources(cleanupContext) {
		r.adopt(journal.CleanupError, failure)
	}
}

func (r *runner) abortOutputs(ctx context.Context) {
	for index := len(r.outputs) - 1; index >= 0; index-- {
		output := r.outputs[index]
		outcome := &r.result.Outputs[output.outcome]
		if output.committed {
			continue
		}
		if output.transaction == nil {
			if output.opened {
				outcome.State = OutputUnknown
			} else {
				outcome.State = OutputAborted
			}
			continue
		}
		if output.commitAttempted {
			outcome.RollbackAttempted = true
		}
		if r.cleanupInvoke(ctx, AbortPhase, outcome.Node, "", output.transaction.Abort) != nil {
			outcome.State = OutputUnknown
			continue
		}
		if output.commitAttempted {
			// Commit may have become visible before returning its error. A
			// successful Abort attempt cannot prove the external outcome.
			outcome.State = OutputUnknown
		} else {
			outcome.State = OutputAborted
		}
	}
}

func (r *runner) closeOperators(ctx context.Context) {
	for index := len(r.opened) - 1; index >= 0; index-- {
		nodeIndex := r.opened[index]
		node := r.nodes[nodeIndex]
		operator := r.operators[nodeIndex]
		r.cleanupInvoke(ctx, ClosePhase, node.ID().String(), "", func(context.Context) error {
			return operator.Close()
		})
	}
}
