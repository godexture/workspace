package host

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/access"
	"github.com/godexture/godec/config"
	"github.com/godexture/godec/internal/bound"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/memory"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
	"github.com/godexture/godec/resource"
)

type directCleanupComponentID struct{}

type panicDirectHandle struct {
	closed atomic.Int32
}

func TestConcurrentPreparedCloseWaitsForOneMemoizedCleanup(t *testing.T) {
	want := errors.New("direct close failed")
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	entry := bound.Direct(plan.Boundary{
		Direction: plan.InputBoundary,
		Kind:      plan.DirectBoundary,
		Node:      "direct",
		Ownership: access.Owned,
	}, struct{}{}, func() error {
		calls.Add(1)
		close(entered)
		<-release
		return want
	})
	prepared := &Prepared{
		manager:        memory.New(resource.Grant{}),
		direct:         []bound.Entry{entry},
		cleanupTimeout: time.Second,
		state:          preparedReady,
		done:           make(chan struct{}),
	}

	first := make(chan error, 1)
	go func() { first <- prepared.Close() }()
	<-entered
	second := make(chan error, 1)
	go func() { second <- prepared.Close() }()
	select {
	case err := <-second:
		close(release)
		t.Fatalf("concurrent Close returned before cleanup completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	firstErr, secondErr := <-first, <-second
	if !errors.Is(firstErr, want) || !errors.Is(secondErr, want) || firstErr.Error() != secondErr.Error() {
		t.Fatalf("Close errors = %v and %v, want the same memoized cleanup failure", firstErr, secondErr)
	}
	if calls.Load() != 1 {
		t.Fatalf("direct Close calls = %d, want exactly one", calls.Load())
	}
}

func TestDirectResourcePanicProjectsAsCleanupPanicUnderZeroSampleBudget(t *testing.T) {
	handle := &panicDirectHandle{}
	resource := access.Own(handle)
	err := resource.Close()
	if err == nil {
		t.Fatal("owned resource panic produced no error")
	}
	ledger := journal.NewBoundedLedger(journal.Budget{Events: 0, GroupSamples: 0, Groups: 1, Stacks: 1, StackBytes: 1 << 20})
	runner := &runner{ctx: context.Background(), ledger: ledger, diag: &diagnosticLog{}}
	runner.adopt(journal.CleanupError, failureOf(ClosePhase, "direct", "direct/close", err))
	runner.collect()
	if len(runner.result.Cleanup) != 1 || !errors.Is(runner.result.Cleanup[0].Err, access.ErrResourceClosePanic) || len(runner.result.Cleanup[0].Stack) == 0 {
		t.Fatalf("cleanup = %#v, want one resolvable panic with stack", runner.result.Cleanup)
	}
	if len(runner.result.Suppressed) != 1 || runner.result.Suppressed[0].Kind != journal.CleanupPanic.String() || runner.result.Suppressed[0].Retained != 0 {
		t.Fatalf("suppressed = %#v, want zero-sample cleanup panic", runner.result.Suppressed)
	}
}

func (h *panicDirectHandle) Close() error {
	h.closed.Add(1)
	panic("direct-close-secret")
}

func TestCloseRequestDirectsAttemptsEveryPanicCloser(t *testing.T) {
	inputHandle := &panicDirectHandle{}
	outputHandle := &panicDirectHandle{}
	adaptor, err := job.NewAdaptor(plugin.IdentityOf[directCleanupComponentID](), config.NewPatch())
	if err != nil {
		t.Fatal(err)
	}
	input, err := job.InputFromSource(access.Own(inputHandle), adaptor)
	if err != nil {
		t.Fatal(err)
	}
	output, err := job.OutputToSink(access.Own(outputHandle), adaptor)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New([]job.Input{input}, []job.Output{output}, job.Graph{})
	if err != nil {
		t.Fatal(err)
	}

	err = closeRequestDirects(request)
	if err == nil || strings.Contains(err.Error(), "direct-close-secret") {
		t.Fatalf("cleanup error = %v, want redacted aggregate", err)
	}
	if inputHandle.closed.Load() != 1 || outputHandle.closed.Load() != 1 {
		t.Fatalf("cleanup calls = input %d, output %d; want one each", inputHandle.closed.Load(), outputHandle.closed.Load())
	}
}

func TestPreparedCloseRetainsStructuredDirectPanicFailures(t *testing.T) {
	var first, second atomic.Int32
	entry := func(node string, choice int, count *atomic.Int32) bound.Entry {
		return bound.Direct(plan.Boundary{
			Direction: plan.InputBoundary,
			Kind:      plan.DirectBoundary,
			Choice:    choice,
			Node:      node,
			Port:      "port",
			Component: "component",
			Ownership: access.Owned,
		}, struct{}{}, func() error {
			count.Add(1)
			panic("prepared-direct-close-secret")
		})
	}
	prepared := &Prepared{
		manager:        memory.New(resource.Grant{}),
		direct:         []bound.Entry{entry("first", 0, &first), entry("second", 1, &second)},
		cleanupTimeout: time.Second,
		state:          preparedReady,
		done:           make(chan struct{}),
	}

	err := prepared.Close()
	if err == nil || strings.Contains(err.Error(), "prepared-direct-close-secret") {
		t.Fatalf("Prepared.Close error = %v, want redacted aggregate", err)
	}
	var failure Failure
	if !errors.As(err, &failure) {
		t.Fatalf("Prepared.Close error = %v, want structured Failure", err)
	}
	if failure.Phase != ClosePhase || failure.Node != "second" || failure.Task != "direct/close" || len(failure.Stack) == 0 {
		t.Fatalf("structured direct failure = %#v, want close phase/node/task/stack", failure)
	}
	if first.Load() != 1 || second.Load() != 1 {
		t.Fatalf("direct cleanup calls = first %d, second %d; want one each", first.Load(), second.Load())
	}
	secondErr := prepared.Close()
	if secondErr == nil || secondErr.Error() != err.Error() {
		t.Fatalf("second Prepared.Close error = %v, want retained first error %v", secondErr, err)
	}
	if first.Load() != 1 || second.Load() != 1 {
		t.Fatalf("direct cleanup calls after second Close = first %d, second %d; want one each", first.Load(), second.Load())
	}
}
