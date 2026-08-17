// Package journal is the failure evidence of one run. A Ledger stores every
// failure the run produced, a Domain is the stable place an ownership slot
// reports a release it could not perform, and a Span is one lifecycle
// operation's recovery boundary and attribution.
package journal

import (
	"fmt"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/errorx"
)

// recoveredSummary describes a recovered value without keeping it.
func recoveredSummary(recovered any) string { return diagnostic.Recovered(recovered) }

// Kind separates what stopped the work from what could not be released or
// closed afterwards, and each of those from the panic form it arrived in.
type Kind uint8

const (
	WorkError Kind = iota + 1
	WorkPanic
	CleanupError
	CleanupPanic
)

func (k Kind) String() string {
	switch k {
	case WorkError:
		return "error"
	case WorkPanic:
		return "panic"
	case CleanupError:
		return "cleanup error"
	case CleanupPanic:
		return "cleanup panic"
	default:
		return "unknown"
	}
}

// Cleanup reports whether this failure is something that could not be released
// or closed, rather than something that stopped useful work. The two never
// compete: a release that failed while the run was already stopping never
// explains why it stopped.
func (k Kind) Cleanup() bool { return k == CleanupError || k == CleanupPanic }

func (k Kind) panicked() Kind {
	if k.Cleanup() {
		return CleanupPanic
	}
	return WorkPanic
}

// Operation is the lifecycle step a failure belongs to. It is the one
// lifecycle vocabulary in the codebase: Host projects it onto its public Phase
// rather than keeping a second list.
//
// It is metadata about a failure and never part of what makes one unique. A
// Span carries it, and a Span nests -- a bounded edge's drain task performs a
// genuine Flush inside its own Run -- so two failures recorded by one task can
// name different operations.
type Operation uint8

const (
	Prepare Operation = iota + 1
	Open
	Run
	Observation
	Finalize
	Flush
	Sync
	PrepareCommit
	Commit
	Abort
	Close
	Join
	Discard
	Resource
)

func (o Operation) String() string {
	switch o {
	case Prepare:
		return "prepare"
	case Open:
		return "open"
	case Run:
		return "run"
	case Observation:
		return "observation"
	case Finalize:
		return "finalize"
	case Flush:
		return "flush"
	case Sync:
		return "sync"
	case PrepareCommit:
		return "prepare-commit"
	case Commit:
		return "commit"
	case Abort:
		return "abort"
	case Close:
		return "close"
	case Join:
		return "join"
	case Discard:
		return "discard"
	case Resource:
		return "resource"
	default:
		return "unknown"
	}
}

// EventID is what makes two failures different things rather than two readings
// of one.
//
// Both halves come from the Ledger. Run distinguishes ledgers, so an identity
// that escapes one run -- inside a cancellation Cause a caller kept, say --
// cannot be mistaken for an event of another. Seq is that ledger's own
// position, assigned once under its lock, so nothing about a task, a name, an
// operation, a group, or a retry participates in identity: a plugin task group
// and a data task group issue from the same counter, and two tasks that share
// a display name cannot collide.
type EventID struct {
	Run uint64
	Seq uint64
}

func (e EventID) Valid() bool { return e.Run != 0 && e.Seq != 0 }

// Failure is one recorded event. Task and Node say where it happened, for a
// reader; ID says which failure it is, for a consumer that must not report one
// twice; Operation says which lifecycle step it belongs to.
type Failure struct {
	ID        EventID
	Kind      Kind
	Operation Operation
	Task      string
	Node      string
	Err       error
	Stack     []byte
}

func (f Failure) Error() string {
	if f.Node == "" {
		return fmt.Sprintf("task %q %s: %v", f.Task, f.Kind, f.Err)
	}
	return fmt.Sprintf("task %q %s at %s: %v", f.Task, f.Kind, f.Node, f.Err)
}

func (f Failure) Unwrap() error { return f.Err }

// Cause is the single error a cancellation tree can carry. Its Event is the
// ledger identity used for echo suppression; Err and the safe attribution
// snapshot make the carrier self-contained when the ledger's representative-
// event budget omitted that event. Cancellation provenance therefore never
// consumes a second pin table or a second retained event.
//
// Everything downstream of a cancellation observes this exact value --
// context.Cause returns it verbatim, and the codebase's peers return
// context.Cause(ctx) as their own error -- so a boundary that sees it again
// resolves it back to the event it names instead of recording a second one.
// That is what keeps an echo from becoming evidence of a second failure, and
// it works by identity rather than by what the error says, which two
// independent failures can say identically.
type Cause struct {
	Event     EventID
	Err       error
	kind      Kind
	operation Operation
	task      string
	node      string
	stack     []byte
}

func (c *Cause) Error() string {
	if c == nil || c.Err == nil {
		return "failure cause"
	}
	return c.Err.Error()
}
func (c *Cause) Unwrap() error { return c.Err }

func newCause(failure Failure) *Cause {
	return &Cause{
		Event:     failure.ID,
		Err:       failure.Err,
		kind:      failure.Kind,
		operation: failure.Operation,
		task:      failure.Task,
		node:      failure.Node,
		// Failure.Stack is an immutable slice from the ledger's depot. Sharing
		// it keeps Cause snapshots inside the same stack budget rather than
		// copying one stack per propagated Cause.
		stack: failure.Stack,
	}
}

// failureSnapshot reconstructs the event a Cause names when the ledger's
// representative budget did not retain it. The metadata is captured when the
// Cause is created, so the fallback does not invent an operation or location
// from the boundary that happens to observe the echo later.
func (c *Cause) failureSnapshot() Failure {
	if c == nil {
		return Failure{}
	}
	return Failure{
		ID:        c.Event,
		Kind:      c.kind,
		Operation: c.operation,
		Task:      c.task,
		Node:      c.node,
		Err:       c.Err,
		Stack:     c.stack,
	}
}

// OperationOf returns the lifecycle operation that produced a cancellation
// cause. It is bounded and panic-safe through errorx.Find, so callers can
// decide whether a propagated cause belongs to Run, Flush, or another
// operation without looking up a potentially omitted ledger sample.
func OperationOf(err error) Operation {
	cause, ok := errorx.Find[*Cause](err)
	if !ok || cause == nil {
		return 0
	}
	return cause.operation
}

// PanicError preserves where work panicked and the stack it panicked from. It
// does not keep the recovered value: a panic value is chosen by the code that
// panicked and can be the data it was handling, so retaining it would put that
// data into anything that renders this error, including %#v.
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

func stackOf(err error) []byte {
	if stack := errorx.Stack(err); len(stack) != 0 {
		return stack
	}
	if aggregate, ok := errorx.Find[*diagnostic.Error](err); ok && aggregate != nil {
		for _, item := range aggregate.Items() {
			if stack := item.Detail["stack"]; stack != "" {
				return []byte(stack)
			}
		}
	}
	return nil
}
