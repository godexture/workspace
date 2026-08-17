package host

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/godexture/godec/internal/journal"
)

type hostPanicUnwrap struct{}

func (hostPanicUnwrap) Error() string { return "third-party failure" }
func (hostPanicUnwrap) Unwrap() error { panic("plugin unwrap") }

func TestRunnerRecordTreatsPanickingUnwrapAsAnOpaqueFailure(t *testing.T) {
	runner := &runner{ledger: journal.NewLedger()}
	err := hostPanicUnwrap{}
	failure := runner.record(journal.WorkError, journal.Flush, "node", "task", err)
	if failure == nil {
		t.Fatal("record returned no failure")
	}
	if failure.Err != err || failure.Phase != FlushPhase || failure.Node != "node" || failure.Task != "task" {
		t.Fatalf("failure = %#v, want the original opaque error and metadata", failure)
	}
	events := runner.ledger.Events()
	if len(events) != 1 || events[0].Kind != journal.WorkError || events[0].Operation != journal.Flush {
		t.Fatalf("events = %#v, want one flush work occurrence", events)
	}
}

func TestRunnerStopKeepsFlushPhaseAliveForPeerEvidence(t *testing.T) {
	jobContext, jobCancel := context.WithCancelCause(context.Background())
	phaseContext, phaseCancel := context.WithCancelCause(context.Background())
	runner := &runner{cancel: jobCancel, phase: phaseContext, phaseCancel: phaseCancel}
	flush := journal.NewLedger().Domain("flush", "flush")
	cause := flush.Perform(journal.Flush, func(*journal.Span) error {
		return errors.New("flush failed")
	})
	if cause == nil {
		t.Fatal("Flush produced no cancellation cause")
	}
	runner.stop(cause)
	if context.Cause(jobContext) == nil {
		t.Fatal("Flush failure did not cancel the job context")
	}
	if context.Cause(phaseContext) != nil {
		t.Fatalf("Flush failure canceled the phase context: %v", context.Cause(phaseContext))
	}
	runner.stop(errors.New("run failed"))
	if context.Cause(phaseContext) == nil {
		t.Fatal("non-Flush failure did not cancel the phase context")
	}
}

func TestPreRunProjectionPreservesPanickingUnwrapAndHidesRecoveredValue(t *testing.T) {
	err := hostPanicUnwrap{}
	value := failureOf(RunPhase, "node", "task", err)
	if value.Err != err || value.Phase != RunPhase || value.Node != "node" || value.Task != "task" {
		t.Fatalf("failureOf = %#v, want the original error and metadata", value)
	}
	returned := invoke(context.Background(), RunPhase, "node", "task", func(context.Context) error { return err })
	if returned == nil || returned.Err != err {
		t.Fatalf("invoke returned = %#v, want the original malformed error", returned)
	}

	const secret = "host-panic-secret"
	panicValue := invoke(context.Background(), RunPhase, "node", "task", func(context.Context) error {
		panic(errors.New(secret))
	})
	if panicValue == nil {
		t.Fatal("panic was not projected")
	}
	if _, ok := panicValue.Err.(*journal.PanicError); !ok {
		t.Fatalf("panic error = %T, want *journal.PanicError", panicValue.Err)
	}
	if strings.Contains(panicValue.Error(), secret) {
		t.Fatal("recovered panic value leaked into the failure")
	}
}
