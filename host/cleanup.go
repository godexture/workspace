package host

import (
	"context"
)

func (r *runner) cleanup() {
	cause := context.Canceled
	if r.result.Primary != nil {
		cause = r.result.Primary
	}
	r.cancel(cause)
	cleanupContext, cancel := context.WithTimeout(context.Background(), r.prepared.cleanupTimeout)
	defer cancel()

	if r.execution != nil {
		if failure := invoke(cleanupContext, ClosePhase, "", "runtime", func(context.Context) error {
			r.execution.Close()
			return nil
		}); failure != nil {
			r.addCleanup(*failure)
		}
	}
	r.data.Cancel(cause)
	r.plugins.Cancel(cause)
	r.acceptTaskReport(r.data.Wait(cleanupContext), true)
	if r.execution != nil {
		if failure := invoke(cleanupContext, ClosePhase, "", "runtime/discard", func(context.Context) error {
			r.execution.Discard(cleanupDomain{runner: r, task: "runtime/discard"})
			return nil
		}); failure != nil {
			r.addCleanup(*failure)
		}
	}
	r.acceptTaskReport(r.plugins.Wait(cleanupContext), true)
	r.abortOutputs(cleanupContext)
	r.closeOperators(cleanupContext)
	for _, failure := range r.prepared.releaseResources(cleanupContext) {
		r.addCleanup(failure)
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
		failure := invoke(ctx, AbortPhase, outcome.Node, "", output.transaction.Abort)
		if failure != nil {
			outcome.State = OutputUnknown
			r.addCleanup(*failure)
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
		failure := invoke(ctx, ClosePhase, node.ID().String(), "", func(context.Context) error {
			return operator.Close()
		})
		if failure != nil {
			r.addCleanup(*failure)
		}
	}
}

func (r *runner) setPrimary(failure Failure) {
	failure = r.observationFailure(failure)
	if r.result.Primary == nil {
		value := failure
		r.result.Primary = &value
		r.diag.failure("host."+string(failure.Phase), failure)
		return
	}
	r.addSecondary(failure)
}

// addCleanup keeps every cleanup failure. Two failures that share an error
// chain are still two things that happened, and suppressing one because it
// resembles the primary drops evidence rather than noise. A cancellation echo
// is removed where it is produced, by the boundary that knows it is one.
func (r *runner) addCleanup(failure Failure) {
	r.result.Cleanup = append(r.result.Cleanup, failure)
	r.diag.failure("host.cleanup."+string(failure.Phase), failure)
}

func (r *runner) addSecondary(failure Failure) {
	r.diag.failure("host.secondary."+string(failure.Phase), failure)
}
