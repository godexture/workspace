package host

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sort"

	"github.com/godexture/godec/diagnostic"
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
