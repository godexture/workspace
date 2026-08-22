package drive

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/schema"
)

type serialCall struct {
	input int
	value int
}

type serialJoiner struct {
	operatorBase
	output schema.Type[owned]

	mu      sync.Mutex
	calls   []serialCall
	flushes atomic.Int32
	failure error
	panic   bool
}

func (j *serialJoiner) Process(ctx context.Context, batch flow.Batch[owned], output flow.Emitter[owned]) error {
	input, selected := batch.Input()
	item := batch.At(0)
	if !selected || batch.Len() != 1 || item == nil || !item.Valid() {
		return errors.New("invalid serial batch")
	}
	if j.panic {
		panic("serial joiner panicked")
	}
	if j.failure != nil {
		return j.failure
	}
	j.mu.Lock()
	j.calls = append(j.calls, serialCall{input: input, value: item.Value().value})
	j.mu.Unlock()
	var result flow.Item[owned]
	output.Own(&result, item.Value())
	defer result.Drop()
	if err := output.Emit(ctx, &result); err != nil {
		return err
	}
	return nil
}

func (j *serialJoiner) Flush(context.Context, flow.Emitter[owned]) error {
	j.flushes.Add(1)
	return nil
}

func (j *serialJoiner) Calls() []serialCall {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]serialCall(nil), j.calls...)
}

func serialShape(in, out schema.Type[owned]) flow.Shape {
	return flow.NewShape(
		[]flow.Port{flow.In("in", in, flow.Many(), flow.WithFanIn(flow.SerialFanIn))},
		[]flow.Port{flow.Out("out", out)},
	)
}

type serialOperator interface {
	flow.Operator
	flow.Joiner[owned, owned]
}

func openSerialFanIn(t testing.TB, inputs int, joiner serialOperator, in, out schema.Type[owned]) ([]Link, Task, *recordingWriter, *journal.Ledger) {
	t.Helper()
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	sink := &recordingWriter{operatorBase: operatorBase{sinkShape}}
	next, err := NewSink("in", out).OpenSink(sink)
	if err != nil {
		t.Fatal(err)
	}
	ledger, owner := testOwner("join")
	links, task, err := NewJoiner("in", in, flow.SerialFanIn, "out", out).OpenJoiner(joiner, inputs, queue.Limit{Items: 1}, 0, next, owner)
	if err != nil {
		t.Fatal(err)
	}
	return links, task, sink, ledger
}

type serialNonConsumerJoiner struct {
	operatorBase
	flushes atomic.Int32
}

func (*serialNonConsumerJoiner) Process(_ context.Context, batch flow.Batch[owned], _ flow.Emitter[owned]) error {
	if input, ok := batch.Input(); !ok || input < 0 || batch.Len() != 1 || batch.At(0) == nil || !batch.At(0).Valid() {
		return errors.New("invalid serial batch")
	}
	return nil
}

func (j *serialNonConsumerJoiner) Flush(context.Context, flow.Emitter[owned]) error {
	j.flushes.Add(1)
	return nil
}

func emitSerial(t testing.TB, link Link, typ schema.Type[owned], value int) {
	t.Helper()
	target, err := deliveryOf[owned](link)
	if err != nil {
		t.Fatal(err)
	}
	item := flow.NewItem(owned{value: value}, typ, &testDomain)
	defer item.Drop()
	if err := target.Emit(context.Background(), &item); err != nil {
		t.Fatal(err)
	}
}

func TestSerialFanInSerializesCallbacksAndInputOrdinal(t *testing.T) {
	in := ownedSchema[driveInputID](&ownership{})
	out := ownedSchema[driveOutputID](&ownership{})
	joiner := &serialJoiner{operatorBase: operatorBase{serialShape(in, out)}, output: out}
	links, task, sink, ledger := openSerialFanIn(t, 2, joiner, in, out)
	if task.Valid() {
		t.Fatal("serial fan-in unexpectedly created a task")
	}
	emitSerial(t, links[1], in, 20)
	emitSerial(t, links[0], in, 10)
	emitSerial(t, links[1], in, 21)
	if err := links[0].Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := joiner.flushes.Load(); got != 0 {
		t.Fatalf("flushes before final input close = %d", got)
	}
	if err := links[1].Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := joiner.Calls(), []serialCall{{input: 1, value: 20}, {input: 0, value: 10}, {input: 1, value: 21}}; !equalSerialCalls(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
	if got := sink.Values(); len(got) != 3 || got[0] != 20 || got[1] != 10 || got[2] != 21 {
		t.Fatalf("sink values = %v", got)
	}
	if got := joiner.flushes.Load(); got != 1 {
		t.Fatalf("flushes = %d, want one", got)
	}
	if err := links[1].Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := joiner.flushes.Load(); got != 1 {
		t.Fatalf("flushes after repeated close = %d, want one", got)
	}
	requireNoFailures(t, ledger)
}

func TestSerialFanInAcceptsOneInputAndRejectsTolerance(t *testing.T) {
	in := ownedSchema[driveInputID](&ownership{})
	out := ownedSchema[driveOutputID](&ownership{})
	joiner := &serialJoiner{operatorBase: operatorBase{serialShape(in, out)}, output: out}
	links, _, sink, _ := openSerialFanIn(t, 1, joiner, in, out)
	emitSerial(t, links[0], in, 7)
	if err := links[0].Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := sink.Values(); len(got) != 1 || got[0] != 7 {
		t.Fatalf("single input values = %v", got)
	}
	if got := joiner.flushes.Load(); got != 1 {
		t.Fatalf("single input flushes = %d", got)
	}

	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	next, err := NewSink("in", out).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	_, owner := testOwner("join")
	binding := NewJoiner("in", in, flow.SerialFanIn, "out", out)
	if _, _, err := binding.OpenJoiner(joiner, 1, queue.Limit{Items: 1}, 1, next, owner); !errors.Is(err, ErrBinding) {
		t.Fatalf("serial fan-in tolerance error = %v", err)
	}
	if _, _, err := binding.OpenJoiner(joiner, 0, queue.Limit{Items: 1}, 0, next, owner); !errors.Is(err, ErrBinding) {
		t.Fatalf("zero serial fan-in inputs error = %v", err)
	}
}

func TestSerialFanInReleasesAnUnconsumedInputOnce(t *testing.T) {
	owners := &ownership{}
	in := ownedSchema[driveInputID](owners)
	out := ownedSchema[driveOutputID](&ownership{})
	joiner := &serialNonConsumerJoiner{operatorBase: operatorBase{serialShape(in, out)}}
	links, _, _, ledger := openSerialFanIn(t, 1, joiner, in, out)
	target, err := deliveryOf[owned](links[0])
	if err != nil {
		t.Fatal(err)
	}
	var item flow.Item[owned]
	target.Own(&item, owned{value: 7})
	if err := target.Emit(context.Background(), &item); err != nil {
		t.Fatal(err)
	}
	item.Drop()
	if got := owners.drops.Load(); got != 1 {
		t.Fatalf("unconsumed input drops = %d, want one", got)
	}
	if err := links[0].Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireNoFailures(t, ledger)
}

type serialReleaseID struct{}

func TestSerialFanInReportsAnUnconsumedDropFailureAndStops(t *testing.T) {
	in := schema.Define[serialReleaseID](schema.Traits[owned]{
		Drop: func(owned) { panic("declared drop panicked") },
	})
	out := ownedSchema[driveOutputID](&ownership{})
	joiner := &serialNonConsumerJoiner{operatorBase: operatorBase{serialShape(in, out)}}
	links, _, _, ledger := openSerialFanIn(t, 1, joiner, in, out)
	target, err := deliveryOf[owned](links[0])
	if err != nil {
		t.Fatal(err)
	}
	var item flow.Item[owned]
	target.Own(&item, owned{value: 1})
	cause := target.Emit(context.Background(), &item)
	assertCauseIsRecorded(t, ledger, cause)
	if item.Valid() {
		t.Fatal("serial runtime left an unconsumed input owned")
	}
	events := ledger.Events()
	if len(events) != 1 || events[0].Kind != journal.CleanupPanic || events[0].Node != "join" {
		t.Fatalf("serial input cleanup = %#v", events)
	}
	blocked := flow.NewItem(owned{value: 2}, in, &testDomain)
	if err := target.Emit(context.Background(), &blocked); !isAbandoned(err) {
		t.Fatalf("callback after cleanup failure = %v", err)
	}
	blocked.Drop()
	if err := links[0].Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := joiner.flushes.Load(); got != 0 {
		t.Fatalf("flushes after cleanup failure = %d", got)
	}
}

func TestSerialFanInProcessFailureSkipsFlushAndDropsCallerItem(t *testing.T) {
	owners := &ownership{}
	in := ownedSchema[driveInputID](owners)
	out := ownedSchema[driveOutputID](&ownership{})
	joiner := &serialJoiner{operatorBase: operatorBase{serialShape(in, out)}, output: out, failure: errors.New("serial Process failed")}
	links, _, _, ledger := openSerialFanIn(t, 2, joiner, in, out)
	target, err := deliveryOf[owned](links[0])
	if err != nil {
		t.Fatal(err)
	}
	item := flow.NewItem(owned{value: 1}, in, &testDomain)
	if err := target.Emit(context.Background(), &item); err == nil {
		t.Fatal("serial Process failure did not stop Emit")
	}
	item.Drop()
	if got := owners.drops.Load(); got != 1 {
		t.Fatalf("caller item drops = %d, want one", got)
	}
	for _, link := range links {
		if err := link.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := joiner.flushes.Load(); got != 0 {
		t.Fatalf("flushes after Process failure = %d", got)
	}
	if events := ledger.Events(); len(events) != 1 || events[0].Node != "join" || events[0].Kind != journal.WorkError {
		t.Fatalf("serial Process attribution = %#v", events)
	}
}

func TestSerialFanInProcessPanicIsAttributedAndSkipsFlush(t *testing.T) {
	owners := &ownership{}
	in := ownedSchema[driveInputID](owners)
	out := ownedSchema[driveOutputID](&ownership{})
	joiner := &serialJoiner{operatorBase: operatorBase{serialShape(in, out)}, output: out, panic: true}
	links, _, _, ledger := openSerialFanIn(t, 1, joiner, in, out)
	target, err := deliveryOf[owned](links[0])
	if err != nil {
		t.Fatal(err)
	}
	item := flow.NewItem(owned{value: 1}, in, &testDomain)
	if err := target.Emit(context.Background(), &item); err == nil {
		t.Fatal("serial Process panic did not stop Emit")
	}
	item.Drop()
	if got := owners.drops.Load(); got != 1 {
		t.Fatalf("caller item drops = %d, want one", got)
	}
	if err := links[0].Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := joiner.flushes.Load(); got != 0 {
		t.Fatalf("flushes after Process panic = %d", got)
	}
	if events := ledger.Events(); len(events) != 1 || events[0].Node != "join" || events[0].Kind != journal.WorkPanic {
		t.Fatalf("serial Process panic attribution = %#v", events)
	}
}

func TestSerialFanInCancellationSkipsFlush(t *testing.T) {
	in := ownedSchema[driveInputID](&ownership{})
	out := ownedSchema[driveOutputID](&ownership{})
	joiner := &serialJoiner{operatorBase: operatorBase{serialShape(in, out)}, output: out}
	links, _, _, ledger := openSerialFanIn(t, 2, joiner, in, out)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := links[0].Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled serial fan-in close = %v", err)
	}
	if err := links[1].Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := joiner.flushes.Load(); got != 0 {
		t.Fatalf("flushes after cancelled close = %d", got)
	}
	requireNoFailures(t, ledger)
}

type blockedSerialJoiner struct {
	operatorBase

	first      sync.Once
	entered    chan struct{}
	release    chan struct{}
	callbacks  chan serialCall
	active     atomic.Int32
	overlapped atomic.Bool
	flushes    atomic.Int32
}

func (j *blockedSerialJoiner) Process(_ context.Context, batch flow.Batch[owned], _ flow.Emitter[owned]) error {
	input, ok := batch.Input()
	item := batch.At(0)
	if !ok || item == nil || !item.Valid() {
		return errors.New("invalid serial batch")
	}
	if j.active.Add(1) != 1 {
		j.overlapped.Store(true)
	}
	first := false
	j.first.Do(func() {
		first = true
		close(j.entered)
	})
	if first {
		<-j.release
	}
	j.callbacks <- serialCall{input: input, value: item.Value().value}
	item.Drop()
	j.active.Add(-1)
	return nil
}

func (j *blockedSerialJoiner) Flush(context.Context, flow.Emitter[owned]) error {
	j.flushes.Add(1)
	return nil
}

func TestSerialFanInDoesNotOverlapConcurrentCallbacks(t *testing.T) {
	in := ownedSchema[driveInputID](&ownership{})
	out := ownedSchema[driveOutputID](&ownership{})
	joinShape := serialShape(in, out)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	next, err := NewSink("in", out).OpenSink(&recordingWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	ledger, owner := testOwner("join")
	joiner := &blockedSerialJoiner{
		operatorBase: operatorBase{joinShape},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
		callbacks:    make(chan serialCall, 2),
	}
	released := false
	defer func() {
		if !released {
			close(joiner.release)
		}
	}()
	links, _, err := NewJoiner("in", in, flow.SerialFanIn, "out", out).OpenJoiner(joiner, 2, queue.Limit{Items: 1}, 0, next, owner)
	if err != nil {
		t.Fatal(err)
	}
	left, err := deliveryOf[owned](links[0])
	if err != nil {
		t.Fatal(err)
	}
	right, err := deliveryOf[owned](links[1])
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	emit := func(target delivery[owned], value int, started chan<- struct{}) {
		item := flow.NewItem(owned{value: value}, in, &testDomain)
		defer item.Drop()
		if started != nil {
			close(started)
		}
		done <- target.Emit(context.Background(), &item)
	}
	go emit(left, 10, nil)
	<-joiner.entered
	started := make(chan struct{})
	go emit(right, 20, started)
	<-started
	select {
	case call := <-joiner.callbacks:
		t.Fatalf("callback overlapped the blocked callback: %#v", call)
	case <-time.After(50 * time.Millisecond):
	}
	close(joiner.release)
	released = true
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if joiner.overlapped.Load() {
		t.Fatal("concurrent serial callbacks overlapped")
	}
	seen := map[serialCall]int{}
	for range 2 {
		seen[<-joiner.callbacks]++
	}
	if len(seen) != 2 || seen[serialCall{input: 0, value: 10}] != 1 || seen[serialCall{input: 1, value: 20}] != 1 {
		t.Fatalf("serial callbacks = %#v", seen)
	}
	for _, link := range links {
		if err := link.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if joiner.flushes.Load() != 1 {
		t.Fatalf("serial fan-in flushes = %d", joiner.flushes.Load())
	}
	requireNoFailures(t, ledger)
}

type serialDropJoiner struct{ operatorBase }

func (*serialDropJoiner) Process(_ context.Context, batch flow.Batch[int], _ flow.Emitter[int]) error {
	batch.At(0).Drop()
	return nil
}

func (*serialDropJoiner) Flush(context.Context, flow.Emitter[int]) error { return nil }

type serialDiscardWriter struct{ operatorBase }

func (*serialDiscardWriter) Write(_ context.Context, item *flow.Item[int]) error {
	item.Drop()
	return nil
}

func TestSerialFanInHopAllocatesZero(t *testing.T) {
	type inputID struct{}
	type outputID struct{}
	in := schema.Define[inputID](schema.Traits[int]{})
	out := schema.Define[outputID](schema.Traits[int]{})
	joinShape := flow.NewShape([]flow.Port{flow.In("in", in, flow.Many(), flow.WithFanIn(flow.SerialFanIn))}, []flow.Port{flow.Out("out", out)})
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", out)}, nil)
	next, err := NewSink("in", out).OpenSink(&serialDiscardWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	_, owner := testOwner("join")
	links, _, err := NewJoiner("in", in, flow.SerialFanIn, "out", out).OpenJoiner(&serialDropJoiner{operatorBase: operatorBase{joinShape}}, 1, queue.Limit{Items: 1}, 0, next, owner)
	if err != nil {
		t.Fatal(err)
	}
	target, err := deliveryOf[int](links[0])
	if err != nil {
		t.Fatal(err)
	}
	var item flow.Item[int]
	allocations := testing.AllocsPerRun(1000, func() {
		target.Own(&item, 1)
		if err := target.Emit(context.Background(), &item); err != nil {
			panic(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("serial fan-in hop allocations = %v", allocations)
	}
}

func equalSerialCalls(got, want []serialCall) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
