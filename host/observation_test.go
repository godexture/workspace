package host

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/observe"
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

func TestObservationFailureKeepsItsIdentityAfterAnEarlierPrimary(t *testing.T) {
	primary := errors.New("data path stopped first")
	observed := errors.New("observation delivery failed later")
	independent := errors.New("another observation failure")
	for name, sink := range map[string]EventSink{
		"error": EventSinkFunc(func(context.Context, Event) error { return observed }),
		"join":  EventSinkFunc(func(context.Context, Event) error { return errors.Join(observed, independent) }),
		"panic": EventSinkFunc(func(context.Context, Event) error { panic("observation delivery panicked later") }),
	} {
		t.Run(name, func(t *testing.T) {
			runner := &runner{
				prepared: &Prepared{cleanupTimeout: time.Second},
				ledger:   journal.NewLedger(),
				diag:     &diagnosticLog{},
			}
			first := runner.record(journal.WorkError, journal.Run, "source", "process", primary)
			if first == nil {
				t.Fatal("the primary failure was not recorded")
			}
			runner.observe = runner.newObservationCollector(runOptions{
				observationSet: true,
				observation: observationOptions{
					mode:     ObservationBasic,
					delivery: 1,
					sink:     sink,
				},
			}, context.Background())
			runner.observe.Emit(structureObservationEvent())
			runner.finishObservation()
			runner.collect()

			if runner.result.Primary == nil || !errors.Is(runner.result.Primary, primary) {
				t.Fatalf("primary = %#v, want the earlier data failure", runner.result.Primary)
			}
			wantSecondary := 1
			if name == "join" {
				wantSecondary = 2
			}
			if len(runner.result.Secondary) != wantSecondary {
				t.Fatalf("secondary = %#v, want the observation failure exactly once", runner.result.Secondary)
			}
			if name == "error" && !errors.Is(runner.result.Secondary[0], observed) {
				t.Fatalf("secondary = %#v, want the observation error", runner.result.Secondary[0])
			}
			if name == "join" && (!errors.Is(runner.result.Secondary[0], observed) || !errors.Is(runner.result.Secondary[1], independent)) {
				t.Fatalf("secondary = %#v, want both independent observation failures", runner.result.Secondary)
			}
			if name == "panic" && len(runner.result.Secondary[0].Stack) == 0 {
				t.Fatal("observation panic lost its stack")
			}
			if got, want := runner.ledger.Occurrences(), uint64(wantSecondary+1); got != want {
				t.Fatalf("ledger occurrences = %d, want one event for each failure", got)
			}
		})
	}
}

func TestDelayedObservationFailureCancelsPhaseBeforeFinish(t *testing.T) {
	jobContext, jobCancel := context.WithCancelCause(context.Background())
	phaseContext, phaseCancel := context.WithCancelCause(context.Background())
	want := errors.New("delayed observation failure")
	runner := &runner{
		ctx:         jobContext,
		cancel:      jobCancel,
		phase:       phaseContext,
		phaseCancel: phaseCancel,
		ledger:      journal.NewLedger(),
		diag:        &diagnosticLog{},
	}
	options := runOptions{
		observationSet: true,
		observation: observationOptions{
			mode:     ObservationBasic,
			delivery: 1,
			sink: EventSinkFunc(func(context.Context, Event) error {
				return want
			}),
		},
	}
	runner.observe = runner.newObservationCollector(options, context.Background())
	runner.observe.Emit(structureObservationEvent())
	select {
	case <-phaseContext.Done():
	case <-time.After(time.Second):
		t.Fatal("observation failure did not cancel the phase context")
	}
	select {
	case <-jobContext.Done():
	case <-time.After(time.Second):
		t.Fatal("observation failure did not cancel the job context")
	}
	if context.Cause(jobContext) == nil {
		t.Fatal("observation failure did not cancel the job context")
	}
	if context.Cause(phaseContext) == nil {
		t.Fatal("observation failure did not cancel the phase context")
	}
	if context.Cause(phaseContext) == context.Canceled {
		t.Fatal("observation failure lost its cause")
	}
	if err := runner.observe.Close(context.Background()); err != nil && !errors.Is(err, want) {
		t.Fatalf("observation close = %v", err)
	}
}

func TestDelayedObservationFailureAfterQuiesceSkipsFlush(t *testing.T) {
	want := errors.New("delayed observation failure")
	state := &lifecycleState{
		flushStarted:  make(chan struct{}),
		flushRelease:  make(chan struct{}),
		flushCanceled: make(chan struct{}),
	}
	instance, request := lifecycleFixture(t, state)
	sink := EventSinkFunc(func(context.Context, Event) error {
		select {
		case <-state.flushStarted:
			return want
		default:
		}
		<-state.flushStarted
		return want
	})
	runDone := make(chan struct {
		result Result
		err    error
	}, 1)
	go func() {
		result, err := instance.Run(context.Background(), request, Observe(ObservationBasic, DeliverEvents(64, sink)))
		runDone <- struct {
			result Result
			err    error
		}{result: result, err: err}
	}()
	select {
	case <-state.flushCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("observation failure did not cancel before Finalize was released")
	}
	close(state.flushRelease)
	select {
	case value := <-runDone:
		if value.err == nil || value.result.Primary == nil || !errors.Is(value.result.Primary, want) {
			t.Fatalf("observation failure = %#v, %v", value.result, value.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop after the delayed observation failure")
	}
	entries, _ := state.snapshot()
	if contains(entries, "flush/sink") {
		t.Fatalf("delayed observation failure allowed a downstream flush: %v", entries)
	}
}

func structureObservationEvent() observe.Event {
	return observe.Event{Kind: observe.Lifecycle, Phase: string(FlushPhase), Message: "complete"}
}

func TestEventSinkJoinUsesCleanupBound(t *testing.T) {
	state := &lifecycleState{}
	instance, request := lifecycleFixture(t, state, CleanupTimeout(20*time.Millisecond))
	started := make(chan struct{})
	release := make(chan struct{})
	lateObserved := make(chan struct{})
	late := errors.New("late renderer failure")
	sink := EventSinkFunc(func(context.Context, Event) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		close(lateObserved)
		return late
	})
	begin := time.Now()
	result, err := instance.Run(t.Context(), request, Observe(ObservationBasic, DeliverEvents(1, sink)))
	elapsed := time.Since(begin)
	<-started
	if err == nil || result.Primary == nil || result.Primary.Phase != ObservationPhase || !errors.Is(result.Primary, context.DeadlineExceeded) {
		t.Fatalf("delivery join result = %#v, %v", result, err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("delivery join exceeded cleanup bound: %s", elapsed)
	}
	before := result
	close(release)
	<-lateObserved
	time.Sleep(20 * time.Millisecond)
	if result.Primary == nil || !errors.Is(result.Primary, context.DeadlineExceeded) || errors.Is(result.Primary, late) {
		t.Fatalf("late sink failure changed returned primary: %#v", result.Primary)
	}
	if !reflect.DeepEqual(result, before) {
		t.Fatalf("late sink failure changed returned result: before=%#v after=%#v", before, result)
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
