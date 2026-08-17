package drive

import (
	"context"
	"errors"
	"math"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/journal"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/timing"
)

type ordered struct {
	value int
	dts   int64
	known bool
}

type mergeRecorder struct {
	operatorBase
	mu      sync.Mutex
	values  []int
	inputs  []int
	flushes atomic.Int32
}

func (j *mergeRecorder) Process(_ context.Context, batch flow.Batch[ordered], _ flow.Emitter[ordered]) error {
	value, ok := batch.Value(0)
	input, selected := batch.InputOrdinal()
	if !ok || !selected || batch.Len() != 1 {
		return errors.New("invalid merge batch")
	}
	j.mu.Lock()
	j.values = append(j.values, value.value)
	j.inputs = append(j.inputs, input)
	j.mu.Unlock()
	batch.At(0).Drop()
	return nil
}

func (j *mergeRecorder) Flush(context.Context, flow.Emitter[ordered]) error {
	j.flushes.Add(1)
	return nil
}

func (j *mergeRecorder) Values() ([]int, []int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]int(nil), j.values...), append([]int(nil), j.inputs...)
}

type orderedWriter struct{ operatorBase }

func (*orderedWriter) Write(_ context.Context, item *flow.Item[ordered]) error {
	item.Drop()
	return nil
}

func orderedSchema[ID any](drops *atomic.Int32) schema.Type[ordered] {
	return schema.Define[ID](schema.Traits[ordered]{
		Drop: func(ordered) {
			if drops != nil {
				drops.Add(1)
			}
		},
		Time:  func(value ordered) (int64, bool) { return value.dts, true },
		Order: func(value ordered) (int64, bool) { return value.dts, value.known },
	})
}

func mergeInputs(count int, limit queue.Limit) []JoinInput {
	inputs := make([]JoinInput, count)
	for index := range inputs {
		inputs[index] = JoinInput{Limit: limit, Base: timing.MustBase(1, 1_000)}
	}
	return inputs
}

func openMerge(t testing.TB, typ schema.Type[ordered], inputs []JoinInput) ([]Link, Task, *mergeRecorder, *journal.Ledger) {
	t.Helper()
	joinShape := flow.NewShape(
		[]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.MergeFanIn))},
		[]flow.Port{flow.Out("out", typ)},
	)
	sinkShape := flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)
	joiner := &mergeRecorder{operatorBase: operatorBase{joinShape}}
	sink, err := NewSink("in", typ).OpenSink(&orderedWriter{operatorBase: operatorBase{sinkShape}})
	if err != nil {
		t.Fatal(err)
	}
	ledger, owner := testOwner("merge")
	links, task, err := NewJoiner("in", typ, flow.MergeFanIn, "out", typ).OpenJoiner(joiner, inputs, 0, sink, owner)
	if err != nil {
		t.Fatal(err)
	}
	producerEnd(links...)
	return links, task, joiner, ledger
}

func emitOrdered(t testing.TB, link Link, typ schema.Type[ordered], value ordered) {
	t.Helper()
	target, err := deliveryOf[ordered](link)
	if err != nil {
		t.Fatal(err)
	}
	item := flow.NewItem(value, typ, &testDomain)
	defer item.Drop()
	if err := target.Emit(context.Background(), &item); err != nil {
		t.Fatal(err)
	}
}

func closeMergeInputs(t testing.TB, inputs []Link) {
	t.Helper()
	for _, input := range inputs {
		if err := input.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func finishMerge(t testing.TB, task Task) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := task.Barrier(ctx); err != nil {
		t.Fatal(err)
	}
	if err := task.Finish(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMergeJoinerOrdersMixedBasesAndDrainsEOF(t *testing.T) {
	type orderedID struct{}
	var drops atomic.Int32
	typ := orderedSchema[orderedID](&drops)
	inputs := []JoinInput{
		{Limit: queue.Limit{Items: 4}, Base: timing.MustBase(1, 1_000)},
		{Limit: queue.Limit{Items: 4}, Base: timing.MustBase(1, 48_000)},
	}
	links, task, joiner, ledger := openMerge(t, typ, inputs)
	emitOrdered(t, links[0], typ, ordered{value: 2, dts: 2, known: true})
	emitOrdered(t, links[1], typ, ordered{value: 1, dts: 48, known: true})
	emitOrdered(t, links[1], typ, ordered{value: 3, dts: 144, known: true})
	closeMergeInputs(t, links)
	if err := perform(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	finishMerge(t, task)
	if err := task.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	values, ordinals := joiner.Values()
	if got, want := values, []int{1, 2, 3}; !slices.Equal(got, want) {
		t.Fatalf("merge values = %v, want %v", got, want)
	}
	if got, want := ordinals, []int{1, 0, 1}; !slices.Equal(got, want) {
		t.Fatalf("merge input ordinals = %v, want %v", got, want)
	}
	if joiner.flushes.Load() != 1 {
		t.Fatalf("merge flushes = %d", joiner.flushes.Load())
	}
	if drops.Load() != 3 {
		t.Fatalf("merge drops = %d", drops.Load())
	}
	requireNoFailures(t, ledger)
}

func TestMergeJoinerAcceptsOneInput(t *testing.T) {
	type singleInputID struct{}
	typ := orderedSchema[singleInputID](nil)
	links, task, joiner, ledger := openMerge(t, typ, mergeInputs(1, queue.Limit{Items: 2}))
	emitOrdered(t, links[0], typ, ordered{value: 1, dts: 1, known: true})
	emitOrdered(t, links[0], typ, ordered{value: 2, dts: 2, known: true})
	closeMergeInputs(t, links)
	if err := perform(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	finishMerge(t, task)
	values, ordinals := joiner.Values()
	if got, want := values, []int{1, 2}; !slices.Equal(got, want) {
		t.Fatalf("merge values = %v, want %v", got, want)
	}
	if got, want := ordinals, []int{0, 0}; !slices.Equal(got, want) {
		t.Fatalf("merge input ordinals = %v, want %v", got, want)
	}
	requireNoFailures(t, ledger)
}

func TestMergeJoinerPreservesOrdinalAndFIFOForEqualOrder(t *testing.T) {
	type equalOrderID struct{}
	typ := orderedSchema[equalOrderID](nil)
	links, task, joiner, ledger := openMerge(t, typ, mergeInputs(2, queue.Limit{Items: 4}))
	emitOrdered(t, links[0], typ, ordered{value: 10, dts: 1, known: true})
	emitOrdered(t, links[0], typ, ordered{value: 11, dts: 1, known: true})
	emitOrdered(t, links[1], typ, ordered{value: 20, dts: 1, known: true})
	closeMergeInputs(t, links)
	if err := perform(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	finishMerge(t, task)
	values, ordinals := joiner.Values()
	if got, want := values, []int{10, 11, 20}; !slices.Equal(got, want) {
		t.Fatalf("merge values = %v, want %v", got, want)
	}
	if got, want := ordinals, []int{0, 0, 1}; !slices.Equal(got, want) {
		t.Fatalf("merge input ordinals = %v, want %v", got, want)
	}
	requireNoFailures(t, ledger)
}

func TestMergeJoinerWaitsForEveryLiveHead(t *testing.T) {
	type waitingID struct{}
	typ := orderedSchema[waitingID](nil)
	links, task, joiner, _ := openMerge(t, typ, mergeInputs(2, queue.Limit{Items: 2}))
	done := make(chan error, 1)
	go func() { done <- perform(context.Background(), task) }()
	emitOrdered(t, links[0], typ, ordered{value: 1, dts: 1, known: true})
	deadline := time.Now().Add(time.Second)
	for links[0].value.(*bufferDelivery[ordered]).queue.Snapshot().Active != 1 {
		if time.Now().After(deadline) {
			t.Fatal("merge did not pop its first head")
		}
		runtime.Gosched()
	}
	if values, _ := joiner.Values(); len(values) != 0 {
		t.Fatalf("merge processed without every head: %v", values)
	}
	emitOrdered(t, links[1], typ, ordered{value: 2, dts: 2, known: true})
	closeMergeInputs(t, links)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	finishMerge(t, task)
}

func TestMergeJoinerRejectsInvalidOrder(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		type missingOrderID struct{}
		typ := orderedSchema[missingOrderID](nil)
		links, task, _, _ := openMerge(t, typ, mergeInputs(1, queue.Limit{Items: 2}))
		emitOrdered(t, links[0], typ, ordered{value: 1})
		closeMergeInputs(t, links)
		if err := perform(context.Background(), task); !errors.Is(err, ErrOrderMissing) {
			t.Fatalf("missing order error = %v", err)
		}
		task.Discard()
	})
	t.Run("backward", func(t *testing.T) {
		type backwardOrderID struct{}
		typ := orderedSchema[backwardOrderID](nil)
		links, task, _, _ := openMerge(t, typ, mergeInputs(1, queue.Limit{Items: 3}))
		emitOrdered(t, links[0], typ, ordered{value: 2, dts: 2, known: true})
		emitOrdered(t, links[0], typ, ordered{value: 1, dts: 1, known: true})
		closeMergeInputs(t, links)
		if err := perform(context.Background(), task); !errors.Is(err, ErrOrderBackward) {
			t.Fatalf("backward order error = %v", err)
		}
		task.Discard()
	})
}

func TestMergeJoinerFailsClosedOnComparisonOverflow(t *testing.T) {
	type overflowOrderID struct{}
	typ := orderedSchema[overflowOrderID](nil)
	inputs := []JoinInput{
		{Limit: queue.Limit{Items: 1}, Base: timing.MustBase(math.MaxInt64, 1)},
		{Limit: queue.Limit{Items: 1}, Base: timing.MustBase(1, math.MaxInt64)},
	}
	links, task, _, ledger := openMerge(t, typ, inputs)
	emitOrdered(t, links[0], typ, ordered{value: 1, dts: math.MaxInt64, known: true})
	emitOrdered(t, links[1], typ, ordered{value: 2, dts: 1, known: true})
	closeMergeInputs(t, links)
	err := perform(context.Background(), task)
	if !errors.Is(err, ErrOrderCompare) {
		t.Fatalf("comparison overflow = %v", err)
	}
	foundOverflow := false
	for _, event := range ledger.Events() {
		foundOverflow = foundOverflow || errors.Is(event.Err, timing.ErrOverflow)
	}
	if !foundOverflow {
		t.Fatalf("comparison overflow was not recorded: %#v", ledger.Events())
	}
	task.Discard()
}

func TestMergeJoinerRejectsMissingTraits(t *testing.T) {
	type untimedID struct{}
	type unorderedID struct{}
	base := timing.MustBase(1, 1_000)
	untimed := schema.Define[untimedID](schema.Traits[ordered]{Order: func(value ordered) (int64, bool) { return value.dts, true }})
	unordered := schema.Define[unorderedID](schema.Traits[ordered]{Time: func(value ordered) (int64, bool) { return value.dts, true }})
	for name, typ := range map[string]schema.Type[ordered]{"untimed": untimed, "unordered": unordered} {
		t.Run(name, func(t *testing.T) {
			shape := flow.NewShape([]flow.Port{flow.In("in", typ, flow.Many(), flow.WithFanIn(flow.MergeFanIn))}, []flow.Port{flow.Out("out", typ)})
			joiner := &mergeRecorder{operatorBase: operatorBase{shape}}
			sink, err := NewSink("in", typ).OpenSink(&orderedWriter{operatorBase: operatorBase{flow.NewShape([]flow.Port{flow.In("in", typ)}, nil)}})
			if err != nil {
				t.Fatal(err)
			}
			_, owner := testOwner("merge")
			if _, _, err := NewJoiner("in", typ, flow.MergeFanIn, "out", typ).OpenJoiner(joiner, []JoinInput{{Limit: queue.Limit{Items: 1}, Base: base}}, 0, sink, owner); !errors.Is(err, ErrBinding) {
				t.Fatalf("missing trait error = %v", err)
			}
		})
	}
}

func TestAbortedMergeReleasesHeldAndQueuedInputsWithoutFlushing(t *testing.T) {
	type abortedID struct{}
	var drops atomic.Int32
	typ := orderedSchema[abortedID](&drops)
	links, task, joiner, ledger := openMerge(t, typ, mergeInputs(2, queue.Limit{Items: 2}))
	done := make(chan error, 1)
	go func() { done <- perform(context.Background(), task) }()
	emitOrdered(t, links[0], typ, ordered{value: 1, dts: 1, known: true})
	deadline := time.Now().Add(time.Second)
	for links[0].value.(*bufferDelivery[ordered]).queue.Snapshot().Active != 1 {
		if time.Now().After(deadline) {
			t.Fatal("merge did not hold its first input")
		}
		runtime.Gosched()
	}
	emitOrdered(t, links[0], typ, ordered{value: 2, dts: 2, known: true})
	task.Abort()
	if err := <-done; err != nil {
		t.Fatalf("aborted merge run = %v", err)
	}
	if err := task.Barrier(context.Background()); err != nil {
		t.Fatalf("aborted merge barrier = %v", err)
	}
	if err := task.Finish(context.Background()); err != nil {
		t.Fatalf("aborted merge finish = %v", err)
	}
	if drops.Load() != 2 {
		t.Fatalf("aborted merge drops = %d", drops.Load())
	}
	if joiner.flushes.Load() != 0 {
		t.Fatalf("aborted merge flushes = %d", joiner.flushes.Load())
	}
	requireNoFailures(t, ledger)
}

func TestMergeCleanupFailurePreventsQuiescence(t *testing.T) {
	type cleanupID struct{}
	typ := schema.Define[cleanupID](schema.Traits[ordered]{
		Drop:  func(ordered) { panic("declared drop panicked") },
		Time:  func(value ordered) (int64, bool) { return value.dts, true },
		Order: func(value ordered) (int64, bool) { return value.dts, value.known },
	})
	links, task, _, ledger := openMerge(t, typ, mergeInputs(1, queue.Limit{Items: 1}))
	emitOrdered(t, links[0], typ, ordered{value: 1, dts: 1, known: true})
	closeMergeInputs(t, links)
	assertCauseIsRecorded(t, ledger, perform(context.Background(), task))
	if len(cleanups(ledger)) != 1 {
		t.Fatalf("cleanup failures = %#v", cleanups(ledger))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := task.Barrier(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("barrier after failed cleanup = %v", err)
	}
}

func TestMergeSelectHeadDoesNotAllocate(t *testing.T) {
	state := mergeState[ordered, ordered]{
		edges:  make([]*queue.Queue[ordered], 3),
		ready:  []bool{true, true, true},
		orders: []int64{3, 48, 2},
		bases: []timing.Base{
			timing.MustBase(1, 1_000),
			timing.MustBase(1, 48_000),
			timing.MustBase(1, 1_000),
		},
	}
	allocations := testing.AllocsPerRun(1_000, func() {
		selected, err := state.selectHead()
		if err != nil || selected != 1 {
			t.Fatalf("selected head = %d, %v", selected, err)
		}
	})
	if allocations != 0 {
		t.Fatalf("merge head selection allocations = %v", allocations)
	}
}
