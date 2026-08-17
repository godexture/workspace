package journal

import (
	"reflect"
	"sync"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/errorx"
)

// Class is what makes two failures the same diagnostic. It is emphatically not
// what makes them the same event: every occurrence keeps its own EventID, and
// echo suppression, re-observation, and cancellation provenance all go by
// EventID alone. Class exists only so a repetition can be counted instead of
// copied.
//
// It is built from bounded, safe structure and never from what an error says.
// An error message can carry a payload or a secret, can vary per occurrence
// until every occurrence is its own class, and can be long enough that storing
// it is itself the problem. A task, a node, an operation, a kind, a Go type and
// a call site are none of those things.
type Class struct {
	Task      string
	Node      string
	Operation Operation
	Kind      Kind
	// Failure is a stable class for the error: a diagnostic code when the error
	// carries one, otherwise its Go type. Both have cardinality bounded by the
	// program rather than by the data flowing through it.
	Failure string
	// Stack is the call site, which separates two failures of one type raised
	// from different places.
	Stack StackID
}

// Group is one failure class the run counted rather than copied.
type Group struct {
	Class Class
	// Count is every occurrence, saturating. It is the number this whole
	// mechanism exists to keep true.
	Count uint64
	First EventID
	Last  EventID
	// Samples are the occurrences kept in full, oldest first. A zero sample
	// budget is valid; Cause keeps its own error and does not depend on this
	// list for echo recognition.
	Samples []EventID
	// Omitted is Count minus what representative Samples kept. The independent
	// stopping provenance can retain one occurrence without changing this
	// sample-budget account.
	Omitted uint64
	// Truncated reports that detail was dropped by budget rather than absent.
	Truncated bool
	// Classes counts the distinct classes folded into this one. It is 1 for an
	// ordinary group and larger for the overflow group a run falls back to when
	// it has seen more classes than it tracks separately. It is exact unless
	// ClassesTruncated says otherwise, in which case it is a lower bound.
	Classes uint64
	// ClassesTruncated reports that more distinct classes were folded in than
	// the run could count, so Classes is a lower bound rather than a total.
	ClassesTruncated bool
}

// coarse is the one global overflow class. Retaining task, operation, or kind
// here would make overflow itself unbounded when a plugin varied any of them.
func (Class) coarse() Class { return Class{Failure: overflowClass} }

// overflowClass names the group distinct classes fold into once the run is
// tracking as many as its budget allows.
const overflowClass = "(classes beyond the ledger budget)"

// Overflow reports whether this group is the coarse one distinct classes fold
// into.
func (g Group) Overflow() bool {
	// A plugin may legitimately choose the same diagnostic code as the
	// internal label. The coarse group is the one that also discarded its
	// operation metadata, so the zero operation disambiguates it.
	return g.Class.Failure == overflowClass && g.Class.Operation == 0
}

// failureClass derives a stable, safe class for an error.
//
// A diagnostic code is preferred because it is the identity the codebase
// already publishes for a failure. Otherwise the concrete Go type is used: it
// says what kind of failure this is without reproducing anything the failure
// was carrying.
func failureClass(err error) string {
	aggregate, ok := errorx.Find[*diagnostic.Error](err)
	if !ok {
		return typeName(err)
	}
	for _, item := range aggregate.Items() {
		if item.Code != "" {
			return item.Code
		}
	}
	return typeName(err)
}

// classNames memoizes type names. A storm reports one class over and over, and
// reflect.Type.String builds a fresh string every time it is asked.
var classNames sync.Map

func typeName(value any) string {
	typ := reflect.TypeOf(value)
	if typ == nil {
		return "unknown"
	}
	if name, known := classNames.Load(typ); known {
		return name.(string)
	}
	name := typ.String()
	classNames.Store(typ, name)
	return name
}
