package journal

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/godexture/godec/internal/errorx"
)

type failingRelease struct{ Token string }

func (f *failingRelease) Error() string      { return "release failed" }
func (f *failingRelease) StackTrace() []byte { return []byte("stack") }

func markedRelease() error {
	return errorx.MarkPanic(&failingRelease{}, []byte("stack"))
}

func TestStackTraceAloneDoesNotClassifyCleanupAsPanic(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("task", "node")
	domain.At("node").Cleanup(&failingRelease{})
	events := ledger.Events()
	if len(events) != 1 || events[0].Kind != CleanupError {
		t.Fatalf("events = %#v, want a cleanup error for an unmarked StackTrace value", events)
	}
}

func TestDomainJoinClassifiesPanicPerOccurrence(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("task", "node")
	ordinary := errors.New("ordinary cleanup failure")
	domain.At("node").Cleanup(errors.Join(ordinary, markedRelease()))
	events := ledger.Events()
	if len(events) != 2 {
		t.Fatalf("events = %#v, want one event per joined child", events)
	}
	if events[0].Kind != CleanupError || !errors.Is(events[0].Err, ordinary) {
		t.Fatalf("ordinary event = %#v, want CleanupError", events[0])
	}
	var release *failingRelease
	if events[1].Kind != CleanupPanic || !errors.As(events[1].Err, &release) {
		t.Fatalf("panic event = %#v, want CleanupPanic", events[1])
	}
}

// A span holds no failures. Ending one cannot make what it recorded
// unreachable, and a domain told about a release after every span has ended
// still reaches the ledger, because the ledger is not a boundary and nothing
// about it ends.
func TestEvidenceOutlivesEverySpanThatRecordedIt(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("task", "node")
	site := domain.At("node")

	run := errors.New("what stopped the run")
	domain.Perform(Run, func(*Span) error {
		site.Cleanup(errors.New("released during Run"))
		return run
	})
	domain.Perform(Flush, func(*Span) error {
		site.Cleanup(errors.New("released during Flush"))
		return nil
	})
	// No span at all: a component releasing what it retained between two
	// lifecycle steps.
	ledger.EnterStage(Close)
	site.Cleanup(errors.New("released with no span open"))

	events := ledger.Events()
	if len(events) != 4 {
		t.Fatalf("events = %#v, want every one kept", events)
	}
	want := []Operation{Run, Run, Flush, Close}
	for index, operation := range want {
		if events[index].Operation != operation {
			t.Errorf("event %d operation = %v, want %v", index, events[index].Operation, operation)
		}
	}
	if !errors.Is(events[1].Err, run) {
		t.Fatalf("event 1 = %v, want the failure that stopped the run", events[1].Err)
	}
}

// Spans nest rather than replace one another, which is how a drain task
// performs a genuine Flush inside the Run it is still executing without
// relabeling anything or letting a second goroutine near its domain.
func TestANestedSpanLabelsOnlyWhatHappensInsideIt(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("edge", "edge")
	site := domain.At("edge")
	domain.Perform(Run, func(*Span) error {
		site.Cleanup(errors.New("during run"))
		domain.Perform(Flush, func(*Span) error {
			site.Cleanup(errors.New("during flush"))
			return nil
		})
		site.Cleanup(errors.New("after the nested flush"))
		return nil
	})
	events := ledger.Events()
	want := []Operation{Run, Flush, Run}
	if len(events) != len(want) {
		t.Fatalf("events = %#v", events)
	}
	for index, operation := range want {
		if events[index].Operation != operation {
			t.Fatalf("event %d operation = %v, want %v", index, events[index].Operation, operation)
		}
	}
}

// Clean answers "did the release I just performed succeed" without reading
// what was recorded, and it is span-relative so a stage cannot be stopped by
// something an earlier operation reported.
func TestCleanTracksWhatThisSpanCaused(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("task", "node")
	site := domain.At("node")
	site.Cleanup(errors.New("before any span"))

	domain.Perform(Run, func(span *Span) error {
		if !span.Clean() {
			t.Error("a new span started dirty from an earlier report")
		}
		site.Cleanup(errors.New("during the span"))
		if span.Clean() {
			t.Error("a span stayed clean after a release failed in it")
		}
		return nil
	})
}

// An operation that ends still holding an unreleased payload has not
// succeeded, so its cause is that release -- but a release never replaces what
// actually stopped the work.
func TestACauseNamesWhatStoppedTheOperation(t *testing.T) {
	for _, test := range []struct {
		name     string
		work     func(*Domain) error
		wantKind Kind
	}{
		{
			name: "a failure stops the work",
			work: func(d *Domain) error {
				d.At("node").Cleanup(errors.New("a release that also failed"))
				return errors.New("what stopped the work")
			},
			wantKind: WorkError,
		},
		{
			name: "only a release failed",
			work: func(d *Domain) error {
				d.At("node").Cleanup(errors.New("the only failure"))
				return nil
			},
			wantKind: CleanupError,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ledger := NewLedger()
			domain := ledger.Domain("task", "node")
			cause := domain.Perform(Run, func(*Span) error { return test.work(domain) })
			var reference *Cause
			if !errors.As(cause, &reference) {
				t.Fatalf("cause = %v, want a reference to a recorded event", cause)
			}
			event, ok := ledger.Event(reference.Event)
			if !ok {
				t.Fatalf("cause names %+v, which the ledger does not hold", reference.Event)
			}
			if event.Kind != test.wantKind {
				t.Fatalf("cause kind = %v, want %v", event.Kind, test.wantKind)
			}
			if operation := OperationOf(cause); operation != Run {
				t.Fatalf("cause operation = %v, want Run", operation)
			}
		})
	}
}

// A panic is described, never kept: the value is chosen by the code that
// panicked and can be the data it was handling.
func TestAPanicNeverCarriesTheValueItChose(t *testing.T) {
	const secret = "journal-panic-secret"
	type credential struct{ Token string }
	ledger := NewLedger()
	domain := ledger.Domain("task", "node")
	domain.Perform(Run, func(*Span) error { panic(credential{Token: secret}) })

	events := ledger.Events()
	if len(events) != 1 || events[0].Kind != WorkPanic {
		t.Fatalf("events = %#v, want the recovered panic", events)
	}
	var panicErr *PanicError
	if !errors.As(events[0].Err, &panicErr) {
		t.Fatalf("event = %v, want a PanicError", events[0].Err)
	}
	if panicErr.Location != "node" || len(panicErr.Stack) == 0 {
		t.Fatalf("panic = %#v, want the node it happened at and the stack it came from", panicErr)
	}
	if strings.Contains(events[0].Error(), secret) {
		t.Error("the recorded failure renders the value the panicking code chose")
	}
}

// A release is attributed to the node that declared the slot, not to wherever
// the domain's goroutine happens to be standing. The stage that declared the
// payload is the stage whose declared Drop failed, and that stays true when
// the release happens deep inside some other stage's call.
func TestAReleaseIsAttributedToTheSiteThatDeclaredTheSlot(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("task", "home")
	upstream := domain.At("upstream")
	domain.Perform(Run, func(*Span) error {
		upstream.Cleanup(errors.New("a payload upstream declared"))
		return nil
	})
	events := ledger.Events()
	if len(events) != 1 || events[0].Node != "upstream" {
		t.Fatalf("release = %#v, want it attributed to the site that declared the slot", events)
	}
}

// Work owned by a domain's outer lifecycle boundary is attributed to its
// immutable home, regardless of which callback path panicked.
func TestAPanicUsesTheDomainHome(t *testing.T) {
	ledger := NewLedger()
	domain := ledger.Domain("task", "home")
	domain.Perform(Run, func(*Span) error {
		panic("inside the callback")
	})
	domain.Perform(Flush, func(*Span) error { return errors.New("returned from the boundary") })

	events := ledger.Events()
	if len(events) != 2 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Node != "home" {
		t.Fatalf("panic node = %q, want the domain home", events[0].Node)
	}
	if events[1].Node != "home" {
		t.Fatalf("returned failure node = %q, want the domain's own node", events[1].Node)
	}
}

func TestSitePerformUsesItsFixedNode(t *testing.T) {
	for name, work := range map[string]func() error{
		"error": func() error { return errors.New("site callback failed") },
		"panic": func() error { panic("site callback panicked") },
	} {
		t.Run(name, func(t *testing.T) {
			ledger := NewLedger()
			site := ledger.Domain("task", "home").At("site")
			site.Perform(work)
			events := ledger.Events()
			if len(events) != 1 || events[0].Node != "site" {
				t.Fatalf("events = %#v, want one site-attributed event", events)
			}
		})
	}
}

func TestSpanStoppingPrefersWorkOverCleanup(t *testing.T) {
	for name, workFirst := range map[string]bool{
		"cleanup then work": false,
		"work then cleanup": true,
	} {
		t.Run(name, func(t *testing.T) {
			ledger := NewLedger()
			domain := ledger.Domain("task", "home")
			site := domain.At("site")
			work := errors.New("work stopped")
			cleanup := errors.New("cleanup failed")
			cause := domain.Perform(Run, func(span *Span) error {
				if workFirst {
					span.Fail(work)
				}
				site.Cleanup(cleanup)
				if !workFirst {
					span.Fail(work)
				}
				return nil
			})
			if cause == nil || !errors.Is(cause, work) {
				t.Fatalf("cause = %v, want work failure", cause)
			}
			if stopped, ok := ledger.Stopping(); !ok || stopped.Kind != WorkError || !errors.Is(stopped.Err, work) {
				t.Fatalf("stopping = %#v, want work failure", stopped)
			}
		})
	}
}

// Recording is safe from every goroutine that can hold a slot, and every event
// keeps its own identity.
func TestConcurrentRecordingKeepsEveryEventAndItsIdentity(t *testing.T) {
	const domains, each = 8, 64
	ledger := NewLedger()
	var wait sync.WaitGroup
	wait.Add(domains)
	for index := range domains {
		go func(index int) {
			defer wait.Done()
			domain := ledger.Domain("worker", "node")
			site := domain.At("node")
			domain.Perform(Run, func(*Span) error {
				for range each {
					site.Cleanup(markedRelease())
				}
				return nil
			})
		}(index)
	}
	wait.Wait()

	if got := ledger.Occurrences(); got != domains*each {
		t.Fatalf("occurrences = %d, want %d", got, domains*each)
	}
	events := ledger.Events()
	seen := make(map[EventID]struct{}, len(events))
	for _, event := range events {
		if _, exists := seen[event.ID]; exists {
			t.Fatalf("identity %+v was issued twice", event.ID)
		}
		seen[event.ID] = struct{}{}
		if event.Kind != CleanupPanic {
			t.Fatalf("kind = %v, want a cleanup panic: the release carried a stack", event.Kind)
		}
	}
}

// A nil domain and a nil site accept everything and lose nothing that was ever
// reachable, because a payload owned outside every domain is a contract
// violation this package cannot prevent and must not compound by panicking on
// the release path.
func TestNilDomainsAndSitesDoNotRaise(t *testing.T) {
	var ledger *Ledger
	var domain *Domain
	var site *Site
	ledger.EnterStage(Close)
	domain.At("node").Cleanup(errors.New("nowhere to go"))
	site.Cleanup(errors.New("nowhere to go"))
	if cause := domain.Perform(Run, func(*Span) error { return nil }); cause != nil {
		t.Fatalf("cause = %v", cause)
	}
	if ledger.Events() != nil || ledger.Failed() || ledger.Stopped() != nil {
		t.Fatal("a nil ledger reported state it does not have")
	}
}
