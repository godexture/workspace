package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/godexture/godec/internal/journal"
)

func newGroup(ctx context.Context) (*Group, *journal.Ledger) {
	ledger := journal.NewLedger()
	return New(ctx, ledger), ledger
}

// failures returns everything the ledger holds that stopped work, which is
// where a task's failure lives now: the group reports what joining found, and
// the ledger reports what happened.
func failures(ledger *journal.Ledger) []journal.Failure {
	var result []journal.Failure
	for _, event := range ledger.Events() {
		if !event.Kind.Cleanup() {
			result = append(result, event)
		}
	}
	return result
}

func TestFailureCancelsPeersAndIsReported(t *testing.T) {
	group, ledger := newGroup(context.Background())
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
	if !report.Complete() {
		t.Fatalf("report = %#v", report)
	}
	// The peer returned ctx.Err() while the group was already cancelled, which
	// is that one failure observed again rather than a second failure.
	recorded := failures(ledger)
	if len(recorded) != 1 || recorded[0].Task != "failure" || !errors.Is(recorded[0].Err, want) {
		t.Fatalf("recorded = %#v", recorded)
	}
	select {
	case <-peerCanceled:
	default:
		t.Fatal("peer did not observe cancellation")
	}
	// Refusing late work while the run is stopping names what stopped it, so a
	// caller reporting the refusal reports that failure rather than a second,
	// unrelated-looking one.
	late := group.Start("late", func(context.Context) error { return nil })
	if !errors.Is(late, want) {
		t.Fatalf("late Start = %v, want the failure the group is stopping for", late)
	}
	var cause *journal.Cause
	if !errors.As(late, &cause) {
		t.Fatalf("late Start = %v, want a reference to the recorded event", late)
	}
	if len(failures(ledger)) != 1 {
		t.Fatalf("recorded = %#v, want the refusal to add nothing", failures(ledger))
	}
}

func TestPanicIsRecoveredAtTaskBoundary(t *testing.T) {
	group, ledger := newGroup(context.Background())
	if err := group.Start("panic", func(context.Context) error { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	group.Wait(context.Background())
	recorded := failures(ledger)
	if len(recorded) != 1 || recorded[0].Kind != journal.WorkPanic {
		t.Fatalf("panic report = %#v", recorded)
	}
	var panicErr *journal.PanicError
	if !errors.As(recorded[0].Err, &panicErr) || !strings.Contains(string(panicErr.Stack), "TestPanicIsRecoveredAtTaskBoundary") {
		t.Fatalf("panic error = %#v", panicErr)
	}
}

// A panic value is chosen by the code that panicked, so it can be the data
// that code was handling. PanicError keeps the stack and a summary instead, and
// this fixes that no rendering of the failure -- %#v over the whole struct
// included -- can bring the value back.
func TestPanicErrorNeverCarriesTheRecoveredValue(t *testing.T) {
	const secret = "task-panic-secret"
	type credential struct{ Token string }
	group, ledger := newGroup(context.Background())
	if err := group.Start("panic", func(context.Context) error { panic(credential{Token: secret}) }); err != nil {
		t.Fatal(err)
	}
	group.Wait(context.Background())
	recorded := failures(ledger)
	var panicErr *journal.PanicError
	if len(recorded) != 1 || !errors.As(recorded[0].Err, &panicErr) {
		t.Fatalf("panic report = %#v", recorded)
	}
	// The rendering itself is never printed: a report that leaks the value
	// would leak it again through the failure message.
	for verb, rendered := range map[string]string{"Error": panicErr.Error(), "%v": fmt.Sprint(panicErr), "%#v": fmt.Sprintf("%#v", *panicErr)} {
		if strings.Contains(rendered, secret) {
			t.Errorf("%s of the panic failure exposes the recovered value", verb)
		}
	}
}

func TestDomainPanicRetainsLocation(t *testing.T) {
	group, ledger := newGroup(context.Background())
	if err := group.StartDomain(ledger.Domain("island", "node"), func(context.Context, *journal.Span) error {
		panic("boom")
	}, nil); err != nil {
		t.Fatal(err)
	}
	group.Wait(context.Background())
	recorded := failures(ledger)
	var panicErr *journal.PanicError
	if len(recorded) != 1 || !errors.As(recorded[0].Err, &panicErr) || panicErr.Location != "node" {
		t.Fatalf("domain panic = %#v", recorded)
	}
	if recorded[0].Node != "node" {
		t.Fatalf("node = %q, want the node the domain belongs to", recorded[0].Node)
	}
}

// A task cannot be started without a domain, because a task without one owns
// slots that report nowhere.
func TestATaskWithoutADomainIsRefused(t *testing.T) {
	group, _ := newGroup(context.Background())
	err := group.StartDomain(nil, func(context.Context, *journal.Span) error { return nil }, nil)
	if !errors.Is(err, ErrDomain) {
		t.Fatalf("StartDomain without a domain = %v, want ErrDomain", err)
	}
}

func TestWaitTimeoutNamesTasksWithoutClaimingTheyStopped(t *testing.T) {
	group, _ := newGroup(context.Background())
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
	group, _ := newGroup(context.Background())
	if report := group.Wait(context.Background()); !report.Complete() {
		t.Fatalf("first report = %#v", report)
	}
	if report := group.Wait(context.Background()); !report.Complete() {
		t.Fatalf("second report = %#v", report)
	}
}

func TestLinkedFailureCancelsOwningContext(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	want := errors.New("linked failure")
	ledger := journal.NewLedger()
	group := NewLinked(ctx, ledger, cancel)
	if err := group.Start("failure", func(context.Context) error { return want }); err != nil {
		t.Fatal(err)
	}
	group.Wait(context.Background())
	if len(failures(ledger)) != 1 || !errors.Is(context.Cause(ctx), want) {
		t.Fatalf("recorded = %#v, cause = %v", failures(ledger), context.Cause(ctx))
	}
	// The cause is a reference to the event, not a copy of it, so a boundary
	// that only ever saw the cancellation can still recover the whole failure.
	var cause *journal.Cause
	if !errors.As(context.Cause(ctx), &cause) {
		t.Fatalf("cause = %#v, want a reference to the event that stopped the group", context.Cause(ctx))
	}
	if event, ok := ledger.Event(cause.Event); !ok || !errors.Is(event.Err, want) {
		t.Fatalf("cause names %+v, which the ledger does not resolve to the failure", cause.Event)
	}
}

func TestExternalCancellationKeepsAnIndependentJoinedFailure(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ledger := journal.NewLedger()
	group := New(parent, ledger)
	independent := errors.New("background task failed while cancellation arrived")
	if err := group.Start("joined", func(ctx context.Context) error {
		<-ctx.Done()
		return errors.Join(ctx.Err(), independent)
	}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if report := group.Wait(context.Background()); !report.Complete() {
		t.Fatalf("report = %#v", report)
	}
	var found bool
	for _, failure := range failures(ledger) {
		if errors.Is(failure.Err, independent) {
			found = true
		}
	}
	if !found {
		t.Fatalf("independent branch disappeared from ledger: %#v", failures(ledger))
	}
}

func TestExternalPureCancellationDoesNotCreateATaskFailure(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ledger := journal.NewLedger()
	group := New(parent, ledger)
	if err := group.Start("peer", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	cancel()
	group.Wait(context.Background())
	if recorded := failures(ledger); len(recorded) != 0 {
		t.Fatalf("pure cancellation became task failures: %#v", recorded)
	}
}
