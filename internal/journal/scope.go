package journal

import (
	"fmt"
	"runtime/debug"
	"sync/atomic"

	"github.com/godexture/godec/diagnostic"
)

// nextAttempt gives every Scope object a process-wide unique Attempt, so two
// Scopes opened for the same Task -- a task's Run journal and the Flush
// journal namedTask.flush opens over the same slots afterward -- never share
// one, regardless of what Operation either was opened with or relabeled to.
var nextAttempt atomic.Uint64

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
// A scope covers exactly one lifecycle operation. Run is one, and the Flush that
// Finish performs over the same slots afterwards is another: whoever performs an
// operation opens its journal, is its only writer, and seals it. Nothing is
// shared between two goroutines and nothing is discarded for arriving late.
type Scope struct {
	operation Operation
	id        uint64
	attempt   uint64
	task      string
	node      string
	next      uint64
	primary   *Failure
	cleanup   []Failure
	sealed    bool
}

// New opens the journal of one lifecycle operation. The goroutine performing
// that operation is its only writer, so a later operation over the same slots
// opens its own rather than sharing this one.
func New(operation Operation, node string) *Scope {
	return &Scope{operation: operation, node: node, attempt: nextAttempt.Add(1)}
}

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

// EnterOperation relabels the failures this scope is about to record and
// returns the label it replaced.
//
// A journal's writer is one goroutine, but that goroutine can pass through
// more than one lifecycle operation itself: a bounded edge's drain task
// performs its own downstream close, a genuine Flush, once its ring reaches
// EOF, in the same call that is about to return to whatever recorded its
// Run failures. Opening a second Scope for that instant would just recreate
// the cross-goroutine read this package exists to avoid, since Host cannot
// tell this goroutine has reached it without watching state this package does
// not expose. Relabeling costs nothing else: identity comes from Task,
// Attempt, and Seq, none of which EnterOperation touches, so relabeling
// cannot manufacture a collision.
func (s *Scope) EnterOperation(operation Operation) Operation {
	if s == nil {
		return 0
	}
	previous := s.operation
	s.operation = operation
	return previous
}

// Cleanup records a release the task could not perform. It is the flow.Reporter
// end of the journal: an ownership slot in this domain reports here instead of
// returning, because releasing happens where no return value is left.
func (s *Scope) Cleanup(err error) {
	if s == nil || err == nil {
		return
	}
	s.cleanup = append(s.cleanup, s.failure(cleanupKind(err), err, stackOf(err)))
}

// Fail records the error that stopped the task. The first one wins: later
// failures are consequences of it, and a task stops once.
func (s *Scope) Fail(err error) {
	if s == nil || err == nil || s.primary != nil {
		return
	}
	failure := s.failure(TaskError, err, stackOf(err))
	s.primary = &failure
}

// Panicked records a recovered panic as what stopped the task. The value is
// described, never kept: it is chosen by the code that panicked and can be the
// data it was handling.
func (s *Scope) Panicked(recovered any, stack []byte) {
	if s == nil {
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

// Seal ends this operation and returns what it came to. It is terminal: a later
// lifecycle operation opens its own journal instead of continuing this one, so
// the writer of each is the single goroutine performing it.
func (s *Scope) Seal() Outcome {
	if s == nil {
		return Outcome{}
	}
	outcome := Outcome{Task: s.task, Primary: s.primary, Cleanup: s.cleanup}
	s.sealed, s.primary, s.cleanup = true, nil, nil
	return outcome
}

// Sealed reports whether the operation this journal covers has ended. Anything
// recorded afterwards belongs to an operation that should have opened its own
// journal, and is still kept: a contract this package cannot enforce is one it
// must not lose evidence of.
func (s *Scope) Sealed() bool { return s != nil && s.sealed }

// Capture runs work under scope and returns what scope ended with, recovering
// any panic and sealing exactly once regardless of how work ends.
//
// This is the one journal boundary every performer of a lifecycle operation
// uses, whether that performer is a task's own goroutine returning from its
// loop or Host reaching across to drive a source's or a join's Finish
// directly. A Scope opened for a Host-driven hand-off is otherwise unattended
// -- nothing else ever recovers a panic from work and seals what it recorded
// -- so without a shared boundary, a panic during that one hand-off would
// discard its journal's evidence exactly the way a task's own panic used to
// discard the return value carrying it.
func Capture(scope *Scope, work func() error) (outcome Outcome) {
	defer func() {
		if recovered := recover(); recovered != nil {
			scope.Panicked(recovered, debug.Stack())
		}
		outcome = scope.Seal()
	}()
	scope.Fail(work())
	return
}

func (s *Scope) failure(kind FailureKind, err error, stack []byte) Failure {
	s.next++
	return Failure{
		Kind:      kind,
		ID:        EventID{Task: s.id, Attempt: s.attempt, Seq: s.next},
		Operation: s.operation,
		Task:      s.task,
		Node:      s.node,
		Err:       err,
		Stack:     append([]byte(nil), stack...),
	}
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

// Attach gives the journal the identity and the name of the task it belongs to.
// The boundary that starts the task does it, because that is where both are
// decided. The identity is the group's, so two tasks that share a name still
// keep their events apart.
func (s *Scope) Attach(id uint64, task string) {
	if s != nil {
		s.id, s.task = id, task
	}
}

// Identity and Name let the next operation over the same slots open a journal
// that still says which task it belongs to.
func (s *Scope) Identity() uint64 {
	if s == nil {
		return 0
	}
	return s.id
}

func (s *Scope) Name() string {
	if s == nil {
		return ""
	}
	return s.task
}
