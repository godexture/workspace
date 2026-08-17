package observe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestOffCreatesNoCounterAndNeverReadsClock(t *testing.T) {
	reads := 0
	collector := New(Off, Config{HistoryLimit: 1}, func() time.Time {
		reads++
		return time.Unix(1, 0)
	})
	if local := collector.Local("node", "edge"); local != nil {
		t.Fatal("Off created a local counter")
	}
	collector.Emit(Event{Kind: Lifecycle})
	if collector != nil || reads != 0 || len(collector.Snapshot()) != 0 {
		t.Fatalf("Off = clock reads %d, events %d", reads, len(collector.Snapshot()))
	}
}

func TestBasicBatchesWithoutClockOrDetailedValues(t *testing.T) {
	reads := 0
	collector := New(Basic, Config{HistoryLimit: 4}, func() time.Time {
		reads++
		return time.Unix(1, 0)
	})
	local := collector.Local("node", "edge")
	local.Add(100, 4, true)
	local.Add(200, 8, true)
	local.Flush()
	events := collector.Snapshot()
	if reads != 0 || len(events) != 1 || events[0].Items != 2 || events[0].Bytes != 0 || events[0].HasMedia || !events[0].At.IsZero() {
		t.Fatalf("Basic events = %#v, clock reads %d", events, reads)
	}
}

func TestDetailedSnapshotsAreImmutable(t *testing.T) {
	now := time.Unix(10, 0)
	collector := New(Detailed, Config{HistoryLimit: 4}, func() time.Time {
		now = now.Add(time.Second)
		return now
	})
	local := collector.Local("node", "edge")
	local.Add(12, 9, true)
	local.Flush()
	collector.Emit(Event{Kind: Diagnostic, Detail: map[string]string{"key": "value"}})
	first := collector.Snapshot()
	first[1].Detail["key"] = "changed"
	second := collector.Snapshot()
	if len(second) != 2 || second[0].Bytes != 12 || second[0].Media != 9 || !second[0].HasMedia || second[1].Detail["key"] != "value" {
		t.Fatalf("Detailed snapshot = %#v", second)
	}
}

func TestTraceEmitsPerItemAndAggregate(t *testing.T) {
	collector := New(Trace, Config{HistoryLimit: 4}, func() time.Time { return time.Unix(1, 0) })
	local := collector.Local("node", "edge")
	local.Add(3, 7, true)
	local.Flush()
	events := collector.Snapshot()
	if len(events) != 2 || events[0].Items != 1 || events[1].Items != 1 {
		t.Fatalf("Trace events = %#v", events)
	}
}

func TestHistoryIsBoundedAndRetainsNewestSequence(t *testing.T) {
	collector := New(Basic, Config{HistoryLimit: 2}, nil)
	for index := range 4 {
		collector.Emit(Event{Kind: Lifecycle, Message: string(rune('a' + index))})
	}
	events := collector.Snapshot()
	summary := collector.Summary()
	if len(events) != 2 || events[0].Sequence != 2 || events[1].Sequence != 3 || summary.HistoryDropped != 2 || summary.DeliveryDropped != 0 {
		t.Fatalf("bounded history = %#v, summary %#v", events, summary)
	}
}

func TestDeliveryOverflowNeverBlocksAndReportsSequenceGap(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan Event, 4)
	collector := New(Basic, Config{
		DeliveryLimit: 1,
		Sink: func(_ context.Context, event Event) error {
			if event.Sequence == 0 {
				close(started)
				<-release
			}
			delivered <- event
			return nil
		},
	}, nil)
	collector.Emit(Event{Kind: Lifecycle})
	<-started
	collector.Emit(Event{Kind: Lifecycle})
	collector.Emit(Event{Kind: Lifecycle})
	close(release)
	first, second := <-delivered, <-delivered
	collector.Emit(Event{Kind: Lifecycle})
	third := <-delivered
	if err := collector.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if sequences := []uint64{first.Sequence, second.Sequence, third.Sequence}; sequences[0] != 0 || sequences[1] != 1 || sequences[2] != 3 {
		t.Fatalf("delivered sequences = %v", sequences)
	}
	if summary := collector.Summary(); summary.DeliveryDropped != 1 {
		t.Fatalf("delivery summary = %#v", summary)
	}
}

// The panic case uses a value carrying a secret because the recovered value
// belongs to the sink and can be whatever it was holding. Keeping it in the
// reported error would publish it through any rendering of that error, %#v
// included.
const sinkPanicSecret = "observe-panic-secret"

type sinkCredential struct{ Token string }

func TestDeliveryFailureAndPanicAreReported(t *testing.T) {
	want := errors.New("renderer failed")
	for name, sink := range map[string]Sink{
		"error": func(context.Context, Event) error { return want },
		"panic": func(context.Context, Event) error { panic(sinkCredential{Token: sinkPanicSecret}) },
	} {
		t.Run(name, func(t *testing.T) {
			failed := make(chan error, 1)
			collector := New(Basic, Config{DeliveryLimit: 1, Sink: sink, Fail: func(err error) { failed <- err }}, nil)
			collector.Emit(Event{Kind: Lifecycle})
			got := <-failed
			if name == "error" && !errors.Is(got, want) {
				t.Fatalf("sink error = %v", got)
			}
			if name == "panic" {
				var panicErr *SinkPanicError
				if !errors.As(got, &panicErr) || len(panicErr.Stack) == 0 {
					t.Fatalf("sink panic = %#v", got)
				}
				for verb, rendered := range map[string]string{"Error": got.Error(), "%v": fmt.Sprint(got), "%#v": fmt.Sprintf("%#v", *panicErr)} {
					if strings.Contains(rendered, sinkPanicSecret) {
						t.Errorf("%s of the sink failure exposes the recovered value", verb)
					}
				}
			}
			if err := collector.Close(t.Context()); !errors.Is(err, got) {
				t.Fatalf("Close = %v, want %v", err, got)
			}
		})
	}
}

func TestCloseTimeoutCommitsFailureBeforeLateSinkError(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	late := errors.New("late sink failure")
	failed := make(chan error, 2)
	collector := New(Basic, Config{
		DeliveryLimit: 2,
		Sink: func(context.Context, Event) error {
			close(started)
			<-release
			return late
		},
		Fail: func(err error) { failed <- err },
	}, nil)
	collector.Emit(Event{Kind: Lifecycle})
	<-started
	collector.Emit(Event{Kind: Lifecycle})
	collector.Emit(Event{Kind: Lifecycle})
	collector.Emit(Event{Kind: Lifecycle})

	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	timeoutErr := collector.Close(ctx)
	if !errors.Is(timeoutErr, context.DeadlineExceeded) {
		t.Fatalf("Close timeout = %v, want deadline exceeded", timeoutErr)
	}
	if summary := collector.Summary(); summary.DeliveryDropped != 3 {
		t.Fatalf("timeout delivery summary = %#v, want all queued and overflow events dropped", summary)
	}

	close(release)
	if err := collector.Close(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late Close = %v, want the terminal timeout", err)
	}
	select {
	case got := <-failed:
		if !errors.Is(got, context.DeadlineExceeded) {
			t.Fatalf("failure callback = %v, want terminal timeout", got)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal failure callback was not called")
	}
	select {
	case got := <-failed:
		t.Fatalf("late sink failure replaced terminal failure: %v", got)
	case <-time.After(20 * time.Millisecond):
	}
	if err := collector.Err(); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, late) {
		t.Fatalf("collector Err after late sink return = %v", err)
	}
}

func TestJoinedCancellationAndSinkFailureRemainDistinct(t *testing.T) {
	parent, cancel := context.WithCancelCause(t.Context())
	started := make(chan struct{})
	release := make(chan struct{})
	independent := errors.New("independent sink failure")
	failed := make(chan error, 1)
	collector := New(Basic, Config{
		Context:       parent,
		DeliveryLimit: 1,
		Sink: func(ctx context.Context, _ Event) error {
			close(started)
			<-release
			return errors.Join(context.Cause(ctx), independent)
		},
		Fail: func(err error) { failed <- err },
	}, nil)
	collector.Emit(Event{Kind: Lifecycle})
	<-started
	cancel(context.Canceled)
	close(release)
	if err := collector.Close(t.Context()); !errors.Is(err, independent) {
		t.Fatalf("Close = %v, want independent sink failure", err)
	}
	select {
	case got := <-failed:
		if !errors.Is(got, independent) {
			t.Fatalf("failure callback = %v, want independent sink failure", got)
		}
	case <-time.After(time.Second):
		t.Fatal("joined sink failure was not reported")
	}
}
