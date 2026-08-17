package cancel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/godexture/godec/internal/errorx"
	"github.com/godexture/godec/internal/journal"
)

type nonComparableError struct{ values []string }

func (e nonComparableError) Error() string { return "non-comparable cancellation" }

func TestLinkUsesOneComparableCarrierForExternalCause(t *testing.T) {
	parent, parentCancel := context.WithCancelCause(context.Background())
	linked, _, detach := Link(parent)
	defer detach()
	parentCancel(nonComparableError{values: []string{"caller"}})
	select {
	case <-linked.Done():
	case <-time.After(time.Second):
		t.Fatal("linked context did not observe parent cancellation")
	}
	cause := context.Cause(linked)
	if cause == nil {
		t.Fatal("linked context has no cause")
	}
	if !errorx.Only(func() error { return context.Cause(linked) }(), cause) {
		t.Fatalf("callback echo %T is not the carrier %T", context.Cause(linked), cause)
	}
	if _, ok := cause.(*carrier); !ok {
		t.Fatalf("cause = %T, want the boundary carrier", cause)
	}
}

func TestLinkKeepsJournalCauseVisibleThroughCarrier(t *testing.T) {
	ledger := journal.NewLedger()
	domain := ledger.Domain("task", "node")
	want := errors.New("flush failed")
	origin := domain.Perform(journal.Flush, func(*journal.Span) error { return want })
	if origin == nil {
		t.Fatal("journal did not produce a cause")
	}
	parent, parentCancel := context.WithCancelCause(context.Background())
	linked, _, detach := Link(parent)
	defer detach()
	parentCancel(origin)
	<-linked.Done()
	if operation := journal.OperationOf(context.Cause(linked)); operation != journal.Flush {
		t.Fatalf("operation = %s, want flush", operation)
	}
	if !errorx.Only(context.Cause(linked), origin) {
		t.Fatalf("carrier did not preserve the journal cause: %v", context.Cause(linked))
	}
}

func TestLinkAlreadyCanceledPreservesValueAndDeadline(t *testing.T) {
	type key struct{}
	deadline := time.Now().Add(time.Minute)
	deadlineContext, cancelDeadline := context.WithDeadline(context.WithValue(context.Background(), key{}, "value"), deadline)
	defer cancelDeadline()
	parent, parentCancel := context.WithCancelCause(deadlineContext)
	parentCancel(nonComparableError{values: []string{"already cancelled"}})
	linked, _, detach := Link(parent)
	defer detach()
	select {
	case <-linked.Done():
	default:
		t.Fatal("already cancelled parent was not propagated before Link returned")
	}
	if got := linked.Value(key{}); got != "value" {
		t.Fatalf("value = %#v, want caller value", got)
	}
	gotDeadline, ok := linked.Deadline()
	if !ok || !gotDeadline.Equal(deadline) {
		t.Fatalf("deadline = %s, %t; want %s, true", gotDeadline, ok, deadline)
	}
	if linked.Err() != context.Canceled {
		t.Fatalf("err = %v, want caller cancellation", linked.Err())
	}
}

func TestLinkPreservesDeadlineExceeded(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer parentCancel()
	linked, _, detach := Link(parent)
	defer detach()
	select {
	case <-linked.Done():
	case <-time.After(time.Second):
		t.Fatal("linked deadline did not expire")
	}
	if linked.Err() != context.DeadlineExceeded {
		t.Fatalf("err = %v, want deadline exceeded", linked.Err())
	}
	if !errorx.Is(context.Cause(linked), context.DeadlineExceeded) {
		t.Fatalf("cause = %v, want wrapped deadline exceeded", context.Cause(linked))
	}
}

func TestLinkKeepsInternalCancellationImmutableAfterParentCancels(t *testing.T) {
	parent, parentCancel := context.WithCancelCause(context.Background())
	linked, cancel, detach := Link(parent)
	defer detach()
	internal := errors.New("internal stop")
	cancel(internal)
	<-linked.Done()
	beforeErr, beforeCause := linked.Err(), context.Cause(linked)
	parentCancel(nonComparableError{values: []string{"later caller cancellation"}})
	if got := linked.Err(); got != beforeErr {
		t.Fatalf("err changed from %v to %v after the link had stopped", beforeErr, got)
	}
	if got := context.Cause(linked); got != beforeCause {
		t.Fatalf("cause identity changed from %T to %T after the link had stopped", beforeCause, got)
	}
}

func TestLinkDetachStopsParentPropagation(t *testing.T) {
	parent, parentCancel := context.WithCancelCause(context.Background())
	linked, cancel, detach := Link(parent)
	detach()
	parentCancel(errors.New("after detached"))
	select {
	case <-linked.Done():
		t.Fatal("detached link still observed its parent")
	case <-time.After(25 * time.Millisecond):
	}
	cancel(nil)
	select {
	case <-linked.Done():
	case <-time.After(time.Second):
		t.Fatal("link cancellation did not work after detach")
	}
}

func TestCarrierIsNilSafe(t *testing.T) {
	var value *carrier
	if value.Error() == "" || value.Unwrap() != nil {
		t.Fatalf("nil carrier = %v / %v", value.Error(), value.Unwrap())
	}
}

type malformedPureError struct{}

func (malformedPureError) Error() string { return "malformed pure error" }
func (malformedPureError) Unwrap() error { panic("malformed unwrap") }

func TestNormalizeRequiresAStoppedContextAndSingleEchoChain(t *testing.T) {
	if normalized := Normalize(context.Background(), context.Canceled); normalized != nil {
		t.Fatalf("a live context normalized cancellation to %v", normalized)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	want := errors.New("stopped")
	cancel(want)
	<-ctx.Done()
	for name, err := range map[string]error{
		"cause":         want,
		"wrapped cause": fmt.Errorf("wrapped: %w", want),
		"canceled":      context.Canceled,
		"deadline":      context.DeadlineExceeded,
	} {
		normalized := Normalize(ctx, err)
		if normalized == nil || !errors.Is(normalized, want) {
			t.Errorf("Normalize(%s) = %v, want trusted cause %v", name, normalized, want)
		}
	}
	for name, err := range map[string]error{
		"nil":       nil,
		"joined":    errors.Join(want, errors.New("independent")),
		"malformed": malformedPureError{},
	} {
		if normalized := Normalize(ctx, err); normalized != nil {
			t.Errorf("Normalize(%s) = %v, want nil", name, normalized)
		}
	}
}

func TestNormalizeRecognizesTheComparableLinkCarrier(t *testing.T) {
	parent, parentCancel := context.WithCancelCause(context.Background())
	linked, _, detach := Link(parent)
	defer detach()
	parentCancel(nonComparableError{values: []string{"caller"}})
	<-linked.Done()
	cause := context.Cause(linked)
	if normalized := Normalize(linked, cause); normalized != cause {
		t.Fatalf("Normalize returned %T, want link carrier %T", normalized, cause)
	}
	if normalized := Normalize(linked, fmt.Errorf("wrapped: %w", cause)); normalized != cause {
		t.Fatalf("Normalize returned %T, want link carrier %T", normalized, cause)
	}
	if normalized := Normalize(linked, nonComparableError{values: []string{"caller"}}); normalized != nil {
		t.Fatalf("the non-comparable source cause was normalized to %v", normalized)
	}
}

func TestNormalizeHighCountAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("high-count cancellation")
	cancel(cause)
	<-ctx.Done()

	const workers = 32
	const rounds = 1000
	var group sync.WaitGroup
	group.Add(workers)
	for index := 0; index < workers; index++ {
		go func() {
			defer group.Done()
			for round := 0; round < rounds; round++ {
				wrapped := fmt.Errorf("provider wrapper: %w", cause)
				if normalized := Normalize(ctx, wrapped); normalized != cause {
					t.Errorf("Normalize returned %v, want trusted cause %v", normalized, cause)
					return
				}
			}
		}()
	}
	group.Wait()
}
