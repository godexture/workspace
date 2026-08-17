package cli

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExitClassificationUsesCallerContextState(t *testing.T) {
	independent := errors.New("plugin runtime failure")
	cleanup := errors.New("cleanup failure")
	joinedCancellation := errors.Join(context.Canceled, independent)

	if got := runtimeExit(context.Background(), joinedCancellation); got != ExitRuntime {
		t.Fatalf("live context classified plugin cancellation evidence as %v, want %v", got, ExitRuntime)
	}
	if got := planningExit(context.Background(), joinedCancellation); got != ExitPlanning {
		t.Fatalf("live context classified planning cancellation evidence as %v, want %v", got, ExitPlanning)
	}
	if got := runtimeExit(nil, joinedCancellation); got != ExitRuntime {
		t.Fatalf("nil context classified plugin cancellation evidence as %v, want %v", got, ExitRuntime)
	}

	canceled, stop := context.WithCancel(context.Background())
	stop()
	<-canceled.Done()
	if got := runtimeExit(canceled, errors.Join(independent, cleanup)); got != ExitCanceled {
		t.Fatalf("caller cancellation classified joined result as %v, want %v", got, ExitCanceled)
	}
	if got := planningExit(canceled, errors.Join(independent, cleanup)); got != ExitCanceled {
		t.Fatalf("caller cancellation classified planning result as %v, want %v", got, ExitCanceled)
	}

	deadline, stopDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stopDeadline()
	<-deadline.Done()
	if got := runtimeExit(deadline, errors.Join(independent, cleanup)); got != ExitCanceled {
		t.Fatalf("caller deadline classified joined result as %v, want %v", got, ExitCanceled)
	}
}
