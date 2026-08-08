package task

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFailureCancelsPeersAndIsReported(t *testing.T) {
	group := New(context.Background())
	want := errors.New("primary")
	peerCanceled := make(chan struct{})
	if err := group.Start("peer", func(ctx context.Context) error {
		<-ctx.Done()
		close(peerCanceled)
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if err := group.Start("failure", func(context.Context) error { return want }); err != nil {
		t.Fatal(err)
	}
	report := group.Wait(context.Background())
	if !report.Complete() || len(report.Failures) != 1 || report.Failures[0].Name != "failure" || !errors.Is(report.Failures[0].Err, want) {
		t.Fatalf("report = %#v", report)
	}
	select {
	case <-peerCanceled:
	default:
		t.Fatal("peer did not observe cancellation")
	}
	if err := group.Start("late", func(context.Context) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("late Start = %v", err)
	}
}

func TestPanicIsRecoveredAtTaskBoundary(t *testing.T) {
	group := New(context.Background())
	if err := group.Start("panic", func(context.Context) error { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	report := group.Wait(context.Background())
	if len(report.Failures) != 1 || !report.Failures[0].Panicked() {
		t.Fatalf("panic report = %#v", report)
	}
	var panicErr *PanicError
	if !errors.As(report.Failures[0].Err, &panicErr) || !strings.Contains(string(panicErr.Stack), "TestPanicIsRecoveredAtTaskBoundary") {
		t.Fatalf("panic error = %#v", panicErr)
	}
}

func TestScopedPanicRunsCleanupAndRetainsLocation(t *testing.T) {
	group := New(context.Background())
	cleaned := false
	if err := group.StartScoped("island", func() string { return "node" }, func() { cleaned = true }, func(context.Context) error {
		panic("boom")
	}); err != nil {
		t.Fatal(err)
	}
	report := group.Wait(context.Background())
	var panicErr *PanicError
	if len(report.Failures) != 1 || !errors.As(report.Failures[0].Err, &panicErr) || panicErr.Location != "node" || !cleaned || panicErr.Cleanup != nil {
		t.Fatalf("scoped panic = %#v, cleaned=%v", report, cleaned)
	}
}

func TestWaitTimeoutNamesTasksWithoutClaimingTheyStopped(t *testing.T) {
	group := New(context.Background())
	release := make(chan struct{})
	defer close(release)
	if err := group.Start("blocked", func(context.Context) error {
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	report := group.Wait(wait)
	if !errors.Is(report.WaitErr, context.DeadlineExceeded) || len(report.Running) != 1 || report.Running[0] != "blocked" || report.Complete() {
		t.Fatalf("timeout report = %#v", report)
	}
}

func TestEmptyGroupWaitIsIdempotent(t *testing.T) {
	group := New(context.Background())
	if report := group.Wait(context.Background()); !report.Complete() {
		t.Fatalf("first report = %#v", report)
	}
	if report := group.Wait(context.Background()); !report.Complete() {
		t.Fatalf("second report = %#v", report)
	}
}
