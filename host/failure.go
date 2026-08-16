package host

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/task"
)

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
			value := failureOf(phase, node, taskName, fmt.Errorf("panic: %s", diagnostic.Recovered(recovered)))
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
	var panicError *journal.PanicError
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
	var stacked interface{ StackTrace() []byte }
	if errors.As(err, &stacked) {
		failure.Stack = stacked.StackTrace()
	}
	for _, item := range diagnostic.ItemsOf(err) {
		if stack := item.Detail["stack"]; stack != "" {
			failure.Stack = []byte(stack)
			break
		}
	}
	return failure
}

// acceptTaskFailure records one journal entry once. cleanup decides which half
// of Result it lands in; primary, when offered, collects the first failure that
// could stop the run.
//
// The key is the event's identity, not what it says. Two releases that failed
// the same way in the same place are two payloads that were not released, and
// reporting one of them would understate what happened.
func (r *runner) acceptTaskFailure(value journal.Failure, cleanup bool, primary **Failure) {
	phase := phaseOf(value.Operation)
	// A task's own failure can be what already stopped the run: Quiesce or
	// WaitSources discovers it through the run's shared cancellation before
	// Host ever reaches this task's journal, and the journal is walked again
	// during cleanup. That second walk is the same event, recognized by the
	// identity Result.Primary's own error chain carries -- comparing EventID
	// against what Primary itself came from, not against context.Cause(ctx)
	// as a stand-in for it. The two agree whenever Primary really is that
	// generic catch, but nothing keeps them in sync when Primary is something
	// else entirely -- a direct chain's own Flush failure, say, discovered
	// through Execution.Finish rather than through cancellation -- and
	// context.Cause(ctx) could still equal a later failure's EventID by being
	// the run's actual, unrelated cancellation cause. Reading the identity off
	// Primary itself only ever matches when Primary truly is that event. A
	// release failure is never this echo: it is not what stopped anything, and
	// Result.Primary is checked first because the first sighting of an event
	// -- before anything has been committed as Primary -- must still be free
	// to become it.
	if !value.Kind.Cleanup() && r.result.Primary != nil {
		var cause *journal.Cause
		if errors.As(r.result.Primary, &cause) && cause.Event == value.ID {
			return
		}
	}
	identity := value.ID
	key := fmt.Sprintf("failure:%d:%d:%d", identity.Task, identity.Attempt, identity.Seq)
	if _, exists := r.reported[key]; exists {
		return
	}
	r.reported[key] = struct{}{}
	failure := failureOf(phase, value.Node, value.Task, value.Err)
	if len(value.Stack) != 0 {
		failure.Stack = append([]byte(nil), value.Stack...)
	}
	switch {
	case cleanup || value.Kind.Cleanup():
		r.addCleanup(failure)
	case primary != nil && *primary == nil:
		*primary = &failure
	default:
		r.addSecondary(failure)
	}
}

// cleanupDomain is the failure domain for releases performed after the data
// tasks have joined and sealed their journals. They belong to the run's
// cleanup, which is where Result already keeps what could not be released.
type cleanupDomain struct {
	runner *runner
	task   string
}

func (d cleanupDomain) Cleanup(err error) {
	d.runner.addCleanup(failureOf(ClosePhase, "", d.task, err))
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

// acceptTaskReport maps what each task ended with onto the two halves Result
// already has. A task's primary competes to be the run's primary; the releases
// it could not perform are the run's cleanup, whichever way the task ended.
func (r *runner) acceptTaskReport(report task.Report, cleanup bool) *Failure {
	outcomes := append([]journal.Outcome(nil), report.Outcomes...)
	sort.SliceStable(outcomes, func(left, right int) bool { return outcomes[left].Task < outcomes[right].Task })
	var primary *Failure
	for _, outcome := range outcomes {
		for _, value := range outcome.Cleanup {
			r.acceptTaskFailure(value, true, nil)
		}
		if outcome.Primary == nil {
			continue
		}
		r.acceptTaskFailure(*outcome.Primary, cleanup, &primary)
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

// acceptOutcomes records what a set of lifecycle operations ended with. Each
// failure names its own operation, so the same plugin failure lands under the
// same phase whether it reached Host through a direct chain's Finish or
// through a bounded edge's own goroutine relabeling itself mid-run. An
// operation's own Primary competes for the run's primary the same way a
// task's Run outcome does, rather than becoming cleanup noise for having
// arrived through Finish instead of through the join; what it could not
// release is always cleanup.
func (r *runner) acceptOutcomes(outcomes []journal.Outcome) *Failure {
	var primary *Failure
	for _, outcome := range outcomes {
		for _, value := range outcome.Cleanup {
			r.acceptTaskFailure(value, true, nil)
		}
		if outcome.Primary == nil {
			continue
		}
		r.acceptTaskFailure(*outcome.Primary, false, &primary)
	}
	return primary
}

// phaseOf places a lifecycle operation in the vocabulary Result reports in.
func phaseOf(operation journal.Operation) Phase {
	switch operation {
	case journal.Flush:
		return FlushPhase
	case journal.Discard:
		return ClosePhase
	default:
		return RunPhase
	}
}
