package host

import (
	"context"

	"github.com/godexture/godec/internal/journal"
)

func (r *runner) runData() *Failure {
	execution, err := r.prepared.program.BuildObserved(r.ledger, r.operators, r.observe)
	if err != nil {
		return r.record(journal.WorkError, journal.Open, "", "runtime/build", err)
	}
	r.execution = execution
	if err := execution.Start(r.data); err != nil {
		return r.record(journal.WorkError, journal.Run, "", "runtime/start", err)
	}
	if err := execution.WaitSources(r.ctx, r.data); err != nil {
		return r.record(journal.WorkError, journal.Run, "", "runtime/source", err)
	}
	if failure := r.invoke(RunPhase, "", "runtime/quiesce", execution.Quiesce); failure != nil {
		return failure
	}
	// Flushing is the run own lifecycle step from here, so a release a
	// component performs on something it retained lands under Flush even when
	// nothing in the runtime is holding a span for it.
	//
	// It is also where a node states what only the whole of its input decides:
	// a coder emits the block it was still filling, and the muxer downstream of
	// it patches sizes that block just changed. One ordered pass carries both,
	// because a node flushes only after every node above it has.
	r.ledger.EnterStage(journal.Flush)
	if err := context.Cause(r.phase); err != nil {
		return r.record(journal.WorkError, journal.Flush, "", "runtime/finish", err)
	}
	// A Flush error or panic already reached the ledger inside the span that
	// performed it. What comes back is the cause to stop on -- a reference to
	// that event -- so there is one path to read rather than a return value
	// racing a separately recovered panic.
	if err := execution.Finish(r.phase); err != nil {
		return r.record(journal.WorkError, journal.Flush, "", "runtime/finish", err)
	}
	report := r.data.Wait(r.phase)
	// A cancelled run gives up this wait immediately, so whatever is still
	// running is still running because the run just stopped. Saying so here
	// would report the cancellation twice over: once as itself and once as
	// tasks that failed to stop, before they were given a bound to stop
	// within. The cancellation is the failure; joining under a bound of its
	// own is cleanup's job, and only what fails to stop by then is a fact
	// about the tasks.
	if err := context.Cause(r.phase); err != nil {
		return r.record(journal.WorkError, journal.Run, "", "", err)
	}
	// A bounded drain performs Flush in its own goroutine. Its Run span can
	// cancel the data group with the Flush cause without returning it through
	// Execution.Finish, so inspect the group's self-contained cause after all
	// data tasks have joined.
	if err := context.Cause(r.data.Context()); err != nil {
		operation := journal.OperationOf(err)
		if operation == 0 {
			operation = journal.Run
		}
		return r.record(journal.WorkError, operation, "", "runtime/finish", err)
	}
	r.ledger.EnterStage(journal.Join)
	if failure := r.acceptTaskReport(report, false); failure != nil {
		return failure
	}
	r.ledger.EnterStage(journal.Discard)
	// The tasks have joined, so what is still queued is discarded rather than
	// delivered. It is still released in the domain of the edge that owned it.
	execution.Discard()
	r.ledger.EnterStage(journal.Commit)
	return nil
}
