package host

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/godexture/godec/diagnostic"
)

func TestObservationConfigurationIsPerRun(t *testing.T) {
	state := &lifecycleState{}
	instance, request := lifecycleFixture(t, state)
	var mu sync.Mutex
	var delivered []Event
	sink := EventSinkFunc(func(_ context.Context, event Event) error {
		mu.Lock()
		delivered = append(delivered, event)
		mu.Unlock()
		return nil
	})
	observed, err := instance.Run(t.Context(), request, Observe(
		ObservationBasic,
		RetainEvents(3),
		DeliverEvents(64, sink),
	))
	if err != nil || !observed.Succeeded() {
		t.Fatalf("observed Run = %#v, %v", observed, err)
	}
	mu.Lock()
	live := append([]Event(nil), delivered...)
	mu.Unlock()
	if len(observed.Events) != 3 || observed.Observation.HistoryDropped == 0 || observed.Observation.DeliveryDropped != 0 || len(live) <= len(observed.Events) {
		t.Fatalf("observed history/live = events %d, live %d, summary %#v", len(observed.Events), len(live), observed.Observation)
	}
	for index := 1; index < len(live); index++ {
		if live[index].Sequence <= live[index-1].Sequence {
			t.Fatalf("live event order = %#v", live)
		}
	}

	unobserved, err := instance.Run(t.Context(), request)
	if err != nil || !unobserved.Succeeded() {
		t.Fatalf("unobserved Run = %#v, %v", unobserved, err)
	}
	if len(unobserved.Events) != 0 || unobserved.Observation != (ObservationSummary{}) {
		t.Fatalf("observation leaked between Runs: %#v", unobserved)
	}
}

func TestSlowEventSinkDoesNotBackpressureMedia(t *testing.T) {
	state := &lifecycleState{}
	instance, request := lifecycleFixture(t, state)
	started := make(chan struct{})
	release := make(chan struct{})
	sink := EventSinkFunc(func(_ context.Context, event Event) error {
		if event.Sequence == 0 {
			close(started)
			<-release
		}
		return nil
	})
	type outcome struct {
		result Result
		err    error
	}
	finished := make(chan outcome, 1)
	go func() {
		result, err := instance.Run(t.Context(), request, Observe(ObservationTrace, DeliverEvents(1, sink)))
		finished <- outcome{result: result, err: err}
	}()
	<-started
	waitForLifecycleEntry(t, state, "commit/sink")
	select {
	case value := <-finished:
		t.Fatalf("Run finished before blocked delivery was joined: %#v, %v", value.result, value.err)
	default:
	}
	close(release)
	value := <-finished
	if value.err != nil || !value.result.Succeeded() || value.result.Observation.DeliveryDropped == 0 {
		t.Fatalf("slow delivery Run = %#v, %v", value.result, value.err)
	}
}

func TestEventSinkFailureAndPanicCancelRunInObservationPhase(t *testing.T) {
	want := errors.New("renderer failed")
	for name, sink := range map[string]EventSink{
		"error": EventSinkFunc(func(context.Context, Event) error { return want }),
		"panic": EventSinkFunc(func(context.Context, Event) error { panic("renderer panic") }),
	} {
		t.Run(name, func(t *testing.T) {
			state := &lifecycleState{}
			instance, request := lifecycleFixture(t, state)
			result, err := instance.Run(t.Context(), request, Observe(ObservationBasic, RetainEvents(8), DeliverEvents(1, sink)))
			if err == nil || result.Primary == nil || result.Primary.Phase != ObservationPhase || result.Primary.Task != "delivery" {
				t.Fatalf("observation failure = %#v, %v", result, err)
			}
			if name == "error" && !errors.Is(result.Primary, want) {
				t.Fatalf("renderer cause = %v", result.Primary)
			}
			if name == "panic" && len(result.Primary.Stack) == 0 {
				t.Fatal("renderer panic did not retain a stack")
			}
			items := result.Diagnostics
			if len(items) == 0 || items[0].Code != "host.observation" {
				t.Fatalf("observation diagnostic = %#v", items)
			}
		})
	}
}

func TestEventSinkJoinUsesCleanupBound(t *testing.T) {
	state := &lifecycleState{}
	instance, request := lifecycleFixture(t, state, CleanupTimeout(20*time.Millisecond))
	started := make(chan struct{})
	release := make(chan struct{})
	sink := EventSinkFunc(func(context.Context, Event) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})
	begin := time.Now()
	result, err := instance.Run(t.Context(), request, Observe(ObservationBasic, DeliverEvents(1, sink)))
	elapsed := time.Since(begin)
	<-started
	close(release)
	if err == nil || result.Primary == nil || result.Primary.Phase != ObservationPhase || !errors.Is(result.Primary, context.DeadlineExceeded) {
		t.Fatalf("delivery join result = %#v, %v", result, err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("delivery join exceeded cleanup bound: %s", elapsed)
	}
}

func TestInvalidObservationOptionDoesNotConsumePrepared(t *testing.T) {
	state := &lifecycleState{}
	instance, request := lifecycleFixture(t, state)
	prepared, err := instance.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := prepared.Run(t.Context(), Observe(ObservationOff, RetainEvents(1)))
	items := diagnostic.ItemsOf(err)
	if result.Primary == nil || result.Primary.Phase != RunPhase || len(items) != 1 || items[0].Code != "host.observation-option" {
		t.Fatalf("invalid observation = %#v, %#v, %v", result, items, err)
	}
	result, err = prepared.Run(t.Context())
	if err != nil || !result.Succeeded() {
		t.Fatalf("retry after invalid option = %#v, %v", result, err)
	}
}

func waitForLifecycleEntry(t testing.TB, state *lifecycleState, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries, _ := state.snapshot()
		if contains(entries, want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("lifecycle did not reach %q", want)
}
