package queue

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godexture/godec/flow"
)

type item struct {
	value int
	size  int
	time  int64
}

func itemTraits() Traits[item] {
	return Traits[item]{
		Size: func(value item) int { return value.size },
		Time: func(value item) (int64, bool) { return value.time, true },
	}
}

// The queue stores cells, so a test hands it one and counts releases through
// the cell rather than through an edge-wide trait.
func pushValue[T any](ctx context.Context, queue *Queue[T], value T, drop func(T)) error {
	cell := flow.NewItemWithTraits(value, nil, drop)
	err := queue.Push(ctx, &cell)
	if err != nil {
		cell.Drop()
	}
	return err
}

func popValue[T any](ctx context.Context, queue *Queue[T]) (T, error) {
	var cell flow.Item[T]
	defer cell.Drop()
	if err := queue.Pop(ctx, &cell); err != nil {
		var zero T
		return zero, err
	}
	return cell.Value(), nil
}

func TestQueueEnforcesItemsBytesAndTime(t *testing.T) {
	queue, err := New(Limit{Items: 4, Bytes: 7, Time: 10}, itemTraits())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, value := range []item{{value: 1, size: 3, time: 20}, {value: 2, size: 4, time: 10}} {
		if err := pushValue(ctx, queue, value, nil); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := queue.Snapshot()
	if snapshot.Items != 2 || snapshot.Bytes != 7 || snapshot.Time != 10 {
		t.Fatalf("queue snapshot = %#v", snapshot)
	}

	pushed := make(chan error, 1)
	go func() { pushed <- pushValue(ctx, queue, item{value: 3, size: 1, time: 15}, nil) }()
	select {
	case err := <-pushed:
		t.Fatalf("bounded push completed early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	value, err := popValue(ctx, queue)
	if err != nil || value.value != 1 {
		t.Fatalf("pop = %#v, %v", value, err)
	}
	if err := <-pushed; err != nil {
		t.Fatal(err)
	}
	if got := queue.Snapshot(); got.Items != 2 || got.Bytes != 5 || got.Time != 5 {
		t.Fatalf("queue after unblock = %#v", got)
	}
}

func TestQueueWaitsRespectCancellationAndClose(t *testing.T) {
	queue, err := New[item](Limit{Items: 1}, Traits[item]{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan error, 1)
	go func() {
		_, err := popValue(ctx, queue)
		wake <- err
	}()
	cancel()
	if err := <-wake; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled pop error = %v", err)
	}
	if err := pushValue(context.Background(), queue, item{value: 1}, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	go func() { wake <- pushValue(ctx, queue, item{value: 2}, nil) }()
	cancel()
	if err := <-wake; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled push error = %v", err)
	}

	queue.Close()
	queue.Close()
	if err := pushValue(context.Background(), queue, item{}, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("push after close error = %v", err)
	}
	closedCause := errors.New("shutdown")
	closedContext, cancelClosed := context.WithCancelCause(context.Background())
	cancelClosed(closedCause)
	if err := pushValue(closedContext, queue, item{}, nil); !errors.Is(err, closedCause) {
		t.Fatalf("canceled push after close error = %v", err)
	}
	value, err := popValue(context.Background(), queue)
	if err != nil || value.value != 1 {
		t.Fatalf("closed queue retained value = %#v, %v", value, err)
	}
	if _, err := popValue(context.Background(), queue); !errors.Is(err, io.EOF) {
		t.Fatalf("closed empty queue error = %v", err)
	}
}

func TestQueueDrainDropsEachOwnerOnce(t *testing.T) {
	var drops atomic.Int32
	count := func(item) { drops.Add(1) }
	queue, err := New(Limit{Items: 8}, itemTraits())
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		if err := pushValue(context.Background(), queue, item{value: index}, count); err != nil {
			t.Fatal(err)
		}
	}
	queue.Close()
	got, err := queue.Drain()
	if got != 5 || err != nil || drops.Load() != 5 {
		t.Fatalf("drain = %d, error %v, drops = %d", got, err, drops.Load())
	}
	if again, err := queue.Drain(); again != 0 || err != nil || drops.Load() != 5 {
		t.Fatal("repeated drain released an owner twice")
	}
}

func TestQueueRejectsUnavailableOrInvalidLimitTraits(t *testing.T) {
	if _, err := New[item](Limit{}, Traits[item]{}); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("invalid limit error = %v", err)
	}
	if _, err := New[item](Limit{Items: 1, Bytes: 1}, Traits[item]{}); !errors.Is(err, ErrSizeTrait) {
		t.Fatalf("missing size trait error = %v", err)
	}
	if _, err := New[item](Limit{Items: 1, Time: 1}, Traits[item]{}); !errors.Is(err, ErrTimeTrait) {
		t.Fatalf("missing time trait error = %v", err)
	}
	queue, _ := New(Limit{Items: 1, Bytes: 1}, Traits[item]{Size: func(item) int { return -1 }})
	if err := pushValue(context.Background(), queue, item{}, nil); !errors.Is(err, ErrInvalidSize) {
		t.Fatalf("invalid size error = %v", err)
	}
	queue, _ = New(Limit{Items: 1, Time: 1}, Traits[item]{Time: func(item) (int64, bool) { return 0, false }})
	if err := pushValue(context.Background(), queue, item{}, nil); !errors.Is(err, ErrUnknownTime) {
		t.Fatalf("unknown time error = %v", err)
	}
}

func TestQueueConcurrentProducersDoNotLoseOrDuplicateItems(t *testing.T) {
	const producers = 4
	const perProducer = 200
	queue, err := New[item](Limit{Items: 17}, Traits[item]{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var producersDone sync.WaitGroup
	producersDone.Add(producers)
	for producer := 0; producer < producers; producer++ {
		go func(producer int) {
			defer producersDone.Done()
			for index := 0; index < perProducer; index++ {
				if err := pushValue(ctx, queue, item{value: producer*perProducer + index}, nil); err != nil {
					t.Errorf("push: %v", err)
					return
				}
			}
		}(producer)
	}
	go func() {
		producersDone.Wait()
		queue.Close()
	}()
	seen := make([]bool, producers*perProducer)
	for {
		value, err := popValue(ctx, queue)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if value.value < 0 || value.value >= len(seen) || seen[value.value] {
			t.Fatalf("duplicate/invalid value %d", value.value)
		}
		seen[value.value] = true
	}
	for index, found := range seen {
		if !found {
			t.Fatalf("missing value %d", index)
		}
	}
}

func TestWaitIdleIncludesDownstreamProcessing(t *testing.T) {
	queue, err := New[item](Limit{Items: 1}, Traits[item]{})
	if err != nil {
		t.Fatal(err)
	}
	if err := pushValue(context.Background(), queue, item{value: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := popValue(context.Background(), queue); err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := queue.WaitIdle(wait); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitIdle before Complete = %v", err)
	}
	queue.Complete()
	if err := queue.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestQueueTransferAllocatesZero(t *testing.T) {
	queue, err := New[int](Limit{Items: 1}, Traits[int]{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	allocations := testing.AllocsPerRun(1000, func() {
		if err := pushValue(ctx, queue, 1, nil); err != nil {
			panic(err)
		}
		if _, err := popValue(ctx, queue); err != nil {
			panic(err)
		}
		queue.Complete()
	})
	if allocations != 0 {
		t.Fatalf("queue transfer allocations = %v", allocations)
	}
}

// A declared Drop belongs to a third party and can panic. Running one while
// holding the ring's mutex would leave it locked forever, and the deferred
// Drain that would otherwise clean up waits on that same mutex, so the panic
// would never reach a recovery boundary.
func TestQueueNeverHoldsItsLockAcrossADeclaredDrop(t *testing.T) {
	queue, err := New[item](Limit{Items: 4}, Traits[item]{})
	if err != nil {
		t.Fatal(err)
	}
	if err := pushValue(context.Background(), queue, item{value: 1}, nil); err != nil {
		t.Fatal(err)
	}
	into := flow.NewItemWithTraits(item{value: 2}, nil, func(item) { panic("declared drop panicked") })
	recovered := func() (value any) {
		defer func() { value = recover() }()
		return queue.Pop(context.Background(), &into)
	}()
	if recovered == nil {
		t.Fatal("the declared drop panic did not propagate")
	}
	if !queue.mu.TryLock() {
		t.Fatal("the queue kept its lock after a declared drop panicked")
	}
	queue.mu.Unlock()
	if _, err := queue.Drain(); err != nil {
		t.Fatalf("drain after the panic failed: %v", err)
	}
}

// Drain releases owners, so one that cannot be released must not strand the
// ones behind it. Every owner is released and the failures are reported
// together rather than as a panic, because Drain runs where no recovery
// boundary is left.
func TestQueueDrainReleasesEveryOwnerDespiteAPanickingDrop(t *testing.T) {
	queue, err := New[item](Limit{Items: 4}, Traits[item]{})
	if err != nil {
		t.Fatal(err)
	}
	var released atomic.Int32
	for index := 0; index < 3; index++ {
		value := item{value: index}
		drop := func(item) {
			released.Add(1)
			if value.value == 0 {
				panic("declared drop panicked")
			}
		}
		if err := pushValue(context.Background(), queue, value, drop); err != nil {
			t.Fatal(err)
		}
	}
	dropped, err := queue.Drain()
	if dropped != 3 {
		t.Fatalf("drain = %d, want 3", dropped)
	}
	if err == nil {
		t.Fatal("a panicking release was not reported")
	}
	if released.Load() != 3 {
		t.Fatalf("released owners = %d, want every owner released", released.Load())
	}
}
