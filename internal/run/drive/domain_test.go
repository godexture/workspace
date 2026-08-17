package drive

import (
	"context"
	"errors"
	"testing"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
)

// testDomain is the failure domain a test holds for the slots it owns itself.
var testDomain flow.Collector

// testOwner opens the failure domain one task would own, on a ledger the test
// reads afterwards. A task constructor takes one, so there is no test that
// builds a task without deciding where its slots report.
func testOwner(name string) (*journal.Ledger, *journal.Domain) {
	ledger := journal.NewLedger()
	return ledger, ledger.Domain(name, name)
}

// perform runs a task the way the group would: one Run span on its own domain,
// recovering a panic, recording what stopped it, and running the sealed hook
// only after that span has ended. It returns the cause and leaves everything
// else in the ledger.
func perform(ctx context.Context, value Task) error {
	cause := value.Domain().Perform(journal.Run, func(span *journal.Span) error {
		return value.Run(ctx, span)
	})
	if sealed := value.Sealed(); sealed != nil {
		sealed(cause)
	}
	return cause
}

// producerEnd binds a bounded edge's producer side, which belongs to whatever
// fills it rather than to the task draining the edge. A test that pushes into
// an edge itself is that producer.
func producerEnd(inputs ...Link) {
	ledger := journal.NewLedger()
	for _, link := range inputs {
		link.bind(ledger.Domain("producer", "producer"))
	}
}

// cleanups returns the releases recorded in a ledger, which is where a slot
// reports one it could not perform.
func cleanups(ledger *journal.Ledger) []journal.Failure {
	var result []journal.Failure
	for _, event := range ledger.Events() {
		if event.Kind.Cleanup() {
			result = append(result, event)
		}
	}
	return result
}

func failuresOf(ledger *journal.Ledger) []journal.Failure {
	var result []journal.Failure
	for _, event := range ledger.Events() {
		if !event.Kind.Cleanup() {
			result = append(result, event)
		}
	}
	return result
}

// assertCauseIsRecorded fixes that a cause is a reference to an event this
// ledger holds rather than a description of one. A caller that only ever sees
// the cancellation can still recover the whole failure through it.
func assertCauseIsRecorded(t testing.TB, ledger *journal.Ledger, cause error) {
	t.Helper()
	if cause == nil {
		t.Fatal("the operation ended without a cause")
	}
	var reference *journal.Cause
	if !errors.As(cause, &reference) {
		t.Fatalf("cause = %v, want a reference to a recorded event", cause)
	}
	if _, ok := ledger.Event(reference.Event); !ok {
		t.Fatalf("cause names %+v, which this ledger does not hold", reference.Event)
	}
}

func requireNoFailures(t testing.TB, ledger *journal.Ledger) {
	t.Helper()
	if events := ledger.Events(); len(events) != 0 {
		t.Fatalf("ledger = %#v, want nothing recorded", events)
	}
}
