package journal

import (
	"fmt"

	"github.com/godexture/godec/diagnostic"
)

// PanicError preserves where a task panicked and the stack it panicked from.
// It does not keep the recovered value: a panic value is chosen by the code
// that panicked and can be the data it was handling, so retaining it would put
// that data into anything that renders this error, including %#v.
type PanicError struct {
	Name     string
	Location string
	Summary  string
	Stack    []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("task %q panicked: %s", e.Name, e.Summary)
}

func (e *PanicError) StackTrace() []byte { return append([]byte(nil), e.Stack...) }

// Scope is one task's journal, and the failure domain of every ownership slot
// that task owns. It is the only place a task's result is assembled: returning
// is one way to reach it, and a panic that discards the return does not lose
// what was written here.
//
// It uses no atomics. The task is the only writer, and the boundary that seals
// it runs in that same goroutine, after the task has stopped.
type Scope struct {
	task    string
	node    string
	primary *Failure
	cleanup []Failure
	sealed  bool
}

func NewScope(node string) *Scope { return &Scope{node: node} }

// Node names the last direct-call node the task entered.
func (s *Scope) Node() string {
	if s == nil {
		return ""
	}
	return s.node
}

// Enter records that the task is inside node and returns the node it left, so a
// delivery restores it on the way back out. Failures recorded in between are
// attributed to node.
func (s *Scope) Enter(node string) string {
	if s == nil {
		return ""
	}
	previous := s.node
	s.node = node
	return previous
}

// Cleanup records a release the task could not perform. It is the flow.Reporter
// end of the journal: an ownership slot in this domain reports here instead of
// returning, because releasing happens where no return value is left.
func (s *Scope) Cleanup(err error) {
	if s == nil || err == nil || s.sealed {
		return
	}
	s.cleanup = append(s.cleanup, s.failure(cleanupKind(err), err, stackOf(err)))
}

// Fail records the error that stopped the task. The first one wins: later
// failures are consequences of it, and a task stops once.
func (s *Scope) Fail(err error) {
	if s == nil || err == nil || s.sealed || s.primary != nil {
		return
	}
	failure := s.failure(TaskError, err, stackOf(err))
	s.primary = &failure
}

// Panicked records a recovered panic as what stopped the task. The value is
// described, never kept: it is chosen by the code that panicked and can be the
// data it was handling.
func (s *Scope) Panicked(recovered any, stack []byte) {
	if s == nil || s.sealed {
		return
	}
	failure := s.failure(TaskPanic, &PanicError{
		Name:     s.task,
		Location: s.node,
		Summary:  diagnostic.Recovered(recovered),
		Stack:    append([]byte(nil), stack...),
	}, stack)
	s.primary = &failure
}

// Clean reports whether nothing has been recorded yet. A stage asks this when
// its own completion depends on the cleanup around it having succeeded, without
// reaching into what was recorded.
func (s *Scope) Clean() bool {
	return s == nil || (s.primary == nil && len(s.cleanup) == 0)
}

// Seal closes the journal and returns what the task ended with. Anything
// reported afterwards belongs to a domain that outlives this task, not here.
func (s *Scope) Seal() Outcome {
	if s == nil {
		return Outcome{}
	}
	s.sealed = true
	return Outcome{Task: s.task, Primary: s.primary, Cleanup: s.cleanup}
}

func (s *Scope) failure(kind FailureKind, err error, stack []byte) Failure {
	return Failure{Kind: kind, Task: s.task, Node: s.node, Err: err, Stack: append([]byte(nil), stack...)}
}

// stacked is what a failure that was recovered from a panic carries. flow's
// release error and the observation sink's both keep one, so the journal can
// tell a cleanup panic from a cleanup error without knowing either type.
type stacked interface{ StackTrace() []byte }

func cleanupKind(err error) FailureKind {
	if _, ok := err.(stacked); ok {
		return CleanupPanic
	}
	return CleanupError
}

func stackOf(err error) []byte {
	if value, ok := err.(stacked); ok {
		return value.StackTrace()
	}
	return nil
}

// Attach names the task this journal belongs to. The boundary that starts the
// task does it, because that is where the name is decided.
func (s *Scope) Attach(task string) {
	if s != nil {
		s.task = task
	}
}
