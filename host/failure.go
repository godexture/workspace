package host

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/errorx"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/task"
)

// record puts one failure in the run ledger and returns it.
//
// Every failure of the run goes through here or through an ownership slot's
// own domain, and both end in the same ledger. There is no second path a Host
// lifecycle failure travels while task failures travel another, and no
// per-consumer bookkeeping deciding what has already been reported: the ledger
// holds each event once, so collecting it once is the default rather than
// something a caller has to arrange.
//
// An error that merely re-observes a failure the ledger already holds -- a
// cancellation cause read at the next boundary, a peer returning
// context.Cause(ctx) verbatim -- resolves back to that event instead of
// becoming a second one. Two independent failures that read identically stay
// two events, because identity here is the ledger's, never the error's text or
// its chain.
func (r *runner) record(kind journal.Kind, operation journal.Operation, node, taskName string, err error) *Failure {
	value := r.recordJournal(kind, operation, node, taskName, err, nil)
	if value == nil {
		return nil
	}
	failure := publicFailure(*value)
	return &failure
}

// recordJournal is the only Host-to-Ledger entry point. Keeping the journal
// value here lets callers that need identity (such as observation delivery)
// retain the event before projecting it onto the public Result shape.
func (r *runner) recordJournal(kind journal.Kind, operation journal.Operation, node, taskName string, err error, stack []byte) *journal.Failure {
	return r.ledger.Record(journal.Entry{
		Kind:      kind,
		Operation: operation,
		Task:      taskName,
		Node:      node,
		Err:       err,
		Stack:     stack,
	})
}

// invoke performs one Host lifecycle step, recovering a panic from it, and
// records what stopped it. It returns the recorded failure so the caller can
// stop; the evidence itself is already durable by then.
func (r *runner) invoke(phase Phase, node, taskName string, work func(context.Context) error) *Failure {
	if err := context.Cause(r.ctx); err != nil {
		return r.record(journal.WorkError, operationOf(phase), node, taskName, err)
	}
	return r.attempt(r.ctx, journal.WorkError, phase, node, taskName, work)
}

// cleanupInvoke is invoke for a step that releases rather than works: its
// failures never compete to explain why the run stopped.
func (r *runner) cleanupInvoke(ctx context.Context, phase Phase, node, taskName string, work func(context.Context) error) *Failure {
	return r.attempt(ctx, journal.CleanupError, phase, node, taskName, work)
}

func (r *runner) attempt(ctx context.Context, kind journal.Kind, phase Phase, node, taskName string, work func(context.Context) error) (failure *Failure) {
	operation := operationOf(phase)
	defer func() {
		if recovered := recover(); recovered != nil {
			stack := debug.Stack()
			panicked := journal.WorkPanic
			if kind.Cleanup() {
				panicked = journal.CleanupPanic
			}
			value := r.recordJournal(panicked, operation, node, taskName, panicError(taskName, node, recovered, stack), stack)
			if value != nil {
				recordedFailure := publicFailure(*value)
				failure = &recordedFailure
			}
		}
	}()
	if err := work(ctx); err != nil {
		return r.record(kind, operation, node, taskName, err)
	}
	return nil
}

// adopt records a failure that was assembled outside the ledger.
//
// Prepare runs before a run's ledger exists and Prepared.Close after it has
// been collected, so those paths build failures directly. When a run performs
// the same step -- verifying a source snapshot, releasing a reservation -- the
// failure belongs to the run and joins everything else in the ledger, so there
// is still one collection point rather than a second list to merge.
func (r *runner) adopt(kind journal.Kind, failure Failure) *Failure {
	return r.record(kind, operationOf(failure.Phase), failure.Node, failure.Task, failure.Err)
}

// failureOf builds a failure outside any run.
func failureOf(phase Phase, node, taskName string, err error) Failure {
	if existing, ok := errorx.Find[*Failure](err); ok && existing != nil {
		value := *existing
		value.Stack = append([]byte(nil), existing.Stack...)
		return value
	}
	if panicValue, ok := errorx.Find[*journal.PanicError](err); ok && panicValue != nil {
		if node == "" {
			node = panicValue.Location
		}
		if taskName == "" {
			taskName = panicValue.Name
		}
	}
	return Failure{Phase: phase, Node: node, Task: taskName, Err: err, Stack: errorx.Stack(err)}
}

// invoke is failureOf with a panic boundary, for the same pre-run path.
func invoke(ctx context.Context, phase Phase, node, taskName string, work func(context.Context) error) (failure *Failure) {
	defer func() {
		if recovered := recover(); recovered != nil {
			stack := debug.Stack()
			value := failureOf(phase, node, taskName, panicError(taskName, node, recovered, stack))
			value.Stack = append([]byte(nil), stack...)
			failure = &value
		}
	}()
	if err := work(ctx); err != nil {
		value := failureOf(phase, node, taskName, err)
		return &value
	}
	return nil
}

// publicFailure projects one ledger event onto the shape Result reports in.
// The identity travels with it: a consumer that must not report one failure
// twice can say so by identity rather than by comparing what two errors say.
func publicFailure(value journal.Failure) Failure {
	return Failure{
		ID:    EventID{Run: value.ID.Run, Seq: value.ID.Seq},
		Phase: phaseOf(value.Operation),
		Node:  value.Node,
		Task:  value.Task,
		Err:   value.Err,
		Stack: append([]byte(nil), value.Stack...),
	}
}

func panicError(name, location string, recovered any, stack []byte) *journal.PanicError {
	return &journal.PanicError{
		Name:     name,
		Location: location,
		Summary:  diagnostic.Recovered(recovered),
		Stack:    append([]byte(nil), stack...),
	}
}

// acceptTaskReport records what joining found. The failures the tasks produced
// are already in the ledger, recorded where they happened; only these two
// facts are joining's own to report.
func (r *runner) acceptTaskReport(report task.Report, cleanup bool) *Failure {
	kind := journal.WorkError
	if cleanup {
		kind = journal.CleanupError
	}
	var first *Failure
	for _, name := range report.Running {
		failure := r.record(kind, journal.Join, "", name, errors.New("task is still running after the cleanup bound"))
		if first == nil {
			first = failure
		}
	}
	if report.WaitErr != nil {
		failure := r.record(kind, journal.Join, "", "", report.WaitErr)
		if first == nil {
			first = failure
		}
	}
	return first
}

// collect turns the run ledger into the Result a caller reads. It is the run's
// single collection point, and it runs once, after every task has joined and
// every operator has been closed.
//
// Each event lands in exactly one place, decided by what it is rather than by
// which boundary happened to notice it:
//
//   - Cleanup holds everything that could not be released or closed. A release
//     that failed while the run was already stopping never explains why it
//     stopped.
//   - Primary holds the earliest failure that stopped useful work. Being
//     earliest is what makes it the run's stop reason: everything after it is
//     downstream of it in time, and the events that are merely echoes of it are
//     not separate events at all.
//   - Secondary holds every other independent work failure. Two components can
//     fail at once without either being the other's consequence, and neither
//     disguising one as cleanup nor hiding it in diagnostics would be an honest
//     account of that.
func (r *runner) collect() {
	seen := make(map[journal.EventID]struct{})
	for _, event := range r.ledger.Events() {
		seen[event.ID] = struct{}{}
		r.collectEvent(event)
	}
	if event, ok := r.ledger.Stopping(); ok {
		if _, retained := seen[event.ID]; !retained {
			r.collectEvent(event)
		}
	}
	// Repetition the run counted rather than copied. Only classes that lost
	// detail appear: a failure that happened once is fully described by its own
	// entry above, and saying "1 occurrence, 1 retained" beside it would be
	// noise on every ordinary failing run.
	for _, group := range r.ledger.Groups() {
		retained := uint64(len(group.Samples))
		if group.Count <= retained && !group.Truncated {
			continue
		}
		// The bounded overflow group deliberately drops operation along with
		// task, node, and kind. It is reported as explicitly unknown; every
		// ordinary group must carry a contract operation and goes through the
		// strict table below.
		phase := UnknownPhase
		if !group.Overflow() {
			phase = phaseOf(group.Class.Operation)
		}
		suppressed := Suppressed{
			Phase:            phase,
			Node:             group.Class.Node,
			Task:             group.Class.Task,
			Class:            group.Class.Failure,
			Kind:             group.Class.Kind.String(),
			Occurrences:      group.Count,
			Retained:         retained,
			First:            EventID{Run: group.First.Run, Seq: group.First.Seq},
			Last:             EventID{Run: group.Last.Run, Seq: group.Last.Seq},
			Truncated:        group.Truncated,
			Classes:          group.Classes,
			ClassesTruncated: group.ClassesTruncated,
		}
		r.result.Suppressed = append(r.result.Suppressed, suppressed)
		r.diag.suppressed(suppressed)
	}
}

func (r *runner) collectEvent(event journal.Failure) {
	failure := publicFailure(event)
	switch {
	case event.Kind.Cleanup():
		r.result.Cleanup = append(r.result.Cleanup, failure)
		code := "host.cleanup." + string(failure.Phase)
		if _, ownership := event.Err.(*journal.OwnershipError); ownership {
			code = "host.cleanup.ownership"
		}
		r.diag.failure(code, failure)
	case r.result.Primary == nil:
		value := failure
		r.result.Primary = &value
		r.diag.failure("host."+string(failure.Phase), failure)
	default:
		r.result.Secondary = append(r.result.Secondary, failure)
		r.diag.failure("host.secondary."+string(failure.Phase), failure)
	}
}

// resultError joins every independent failure the run produced. Primary is
// what stopped it, Secondary is what failed alongside it, and Cleanup is what
// could not be released; none of them substitutes for another.
//
// A repetition contributes its representative once plus one statement of how
// many times it happened, never one copy per occurrence: the count belongs in
// structure, and joining a hundred thousand identical errors would be a worse
// way to say the same thing.
func resultError(result Result) error {
	values := make([]error, 0, 1+len(result.Secondary)+len(result.Cleanup)+len(result.Suppressed))
	if result.Primary != nil {
		values = append(values, *result.Primary)
	}
	for _, failure := range result.Secondary {
		values = append(values, failure)
	}
	for _, failure := range result.Cleanup {
		values = append(values, failure)
	}
	for _, suppressed := range result.Suppressed {
		values = append(values, suppressed)
	}
	return errors.Join(values...)
}

// phaseOperation is the one correspondence between the run's lifecycle
// vocabulary and the name Result reports it under. Keeping both directions in
// this table prevents a newly added lifecycle step from silently falling back
// to Run in one direction.
type phaseOperation struct {
	operation journal.Operation
	phase     Phase
}

var phaseOperations = [...]phaseOperation{
	{operation: journal.Prepare, phase: PreparePhase},
	{operation: journal.Open, phase: OpenPhase},
	{operation: journal.Run, phase: RunPhase},
	{operation: journal.Observation, phase: ObservationPhase},
	{operation: journal.Finalize, phase: FinalizePhase},
	{operation: journal.Flush, phase: FlushPhase},
	{operation: journal.Sync, phase: SyncPhase},
	{operation: journal.PrepareCommit, phase: PrepareCommitPhase},
	{operation: journal.Commit, phase: CommitPhase},
	{operation: journal.Abort, phase: AbortPhase},
	{operation: journal.Close, phase: ClosePhase},
	{operation: journal.Join, phase: JoinPhase},
	{operation: journal.Discard, phase: DiscardPhase},
	{operation: journal.Resource, phase: ResourcePhase},
}

func phaseOf(operation journal.Operation) Phase {
	for _, pair := range phaseOperations {
		if pair.operation == operation {
			return pair.phase
		}
	}
	panic(fmt.Sprintf("host: unknown journal operation %d", operation))
}

func operationOf(phase Phase) journal.Operation {
	for _, pair := range phaseOperations {
		if pair.phase == phase {
			return pair.operation
		}
	}
	panic(fmt.Sprintf("host: unknown phase %q", phase))
}
