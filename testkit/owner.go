package testkit

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/host"
	mediaformat "github.com/godexture/godec/media/format"
)

var errRejectedItem = errors.New("testkit rejected an item")

// rejection refuses the first item the subject emits. It is attached to a
// pass-through processor placed directly downstream, because Host queues the
// sink boundary: only a fused neighbour makes the subject observe the failure
// synchronously, which is what separates "consume on success, leave to the
// caller on failure" from "always consume".
type rejection struct {
	seen      atomic.Int32
	triggered atomic.Bool
}

func (r *rejection) accept() error {
	if r == nil {
		return nil
	}
	if r.seen.Add(1) == 1 {
		r.triggered.Store(true)
		return errRejectedItem
	}
	return nil
}

// acceptsRejection excludes write Format subjects. Their downstream consumer
// is the Access sink boundary, whose adjacent component must carry the Format
// write trait that selects the sink capabilities, so a processor cannot be
// placed between them. Emit-failure ownership for that direction is recorded
// as an uncovered contract instead of approximated here.
func acceptsRejection[I, O any](kind runnerKind, subject Subject[I, O]) bool {
	if kind != formatRunner {
		return true
	}
	component, ok := componentOf(subject.set, subject.identity)
	if !ok {
		return false
	}
	_, write := mediaformat.WriteOf(component)
	return !write
}

// runRejected replays a successful case with a rejecting neighbour. The
// subject must report the failure without releasing the input it no longer
// owns, and Host must release everything exactly once.
func runRejected[I, O any](t testing.TB, kind runnerKind, subject Subject[I, O], test Case[I, O], input Fixture[I]) {
	t.Helper()
	reject := &rejection{}
	scenario, err := newScenario(kind, subject, test.Config, input, test.Want.newRecorder(), withRejection(reject))
	if err != nil {
		t.Fatalf("testkit rejection scenario: %v", err)
	}
	defer func() {
		if err := scenario.close(); err != nil {
			t.Errorf("testkit rejection ownership: %v", err)
		}
	}()
	prepared, err := scenario.host.Prepare(context.Background(), scenario.job)
	if err != nil {
		t.Fatalf("testkit rejection Prepare: %v", err)
	}
	result, runErr := prepared.Run(context.Background(), host.VerifyOwnership())
	closeErr := prepared.Close()
	if !reject.triggered.Load() {
		if runErr != nil || closeErr != nil {
			t.Fatalf("testkit rejection scenario produced no output but failed: run=%v close=%v", runErr, closeErr)
		}
		return
	}
	if !errors.Is(runErr, errRejectedItem) {
		t.Errorf("testkit rejection Run error = %v, want %v", runErr, errRejectedItem)
	}
	assertNoIncidentalFailures(t, "testkit rejection", result)
}
