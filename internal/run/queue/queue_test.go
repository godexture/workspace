package queue

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type item struct {
	value int
	size  int
	time  int64
}

func itemTraits(drops *atomic.Int32) Traits[item] {
	return Traits[item]{
		Drop: func(item) {
			if drops != nil {
				drops.Add(1)
			}
		},
		Size: func(value item) int { return value.size },
		Time: func(value item) (int64, bool) { return value.time, true },
	}
}

func TestQueueEnforcesItemsBytesAndTime(t *testing.T) {
	queue, err := New(Limit{Items: 4, Bytes: 7, Time: 10}, itemTraits(nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, value := range []item{{value: 1, size: 3, time: 20}, {value: 2, size: 4, time: 10}} {
		if err := queue.Push(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := queue.Snapshot()
	if snapshot.Items != 2 || snapshot.Bytes != 7 || snapshot.Time != 10 {
		t.Fatalf("queue snapshot = %#v", snapshot)
	}

	pushed := make(chan error, 1)
	go func() { pushed <- queue.Push(ctx, item{value: 3, size: 1, time: 15}) }()
	select {
	case err := <-pushed:
		t.Fatalf("bounded push completed early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	value, err := queue.Pop(ctx)
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
		_, err := queue.Pop(ctx)
		wake <- err
	}()
	cancel()
	if err := <-wake; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled pop error = %v", err)
	}
	if err := queue.Push(context.Background(), item{value: 1}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	go func() { wake <- queue.Push(ctx, item{value: 2}) }()
	cancel()
	if err := <-wake; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled push error = %v", err)
	}

	queue.Close()
	queue.Close()
	if err := queue.Push(context.Background(), item{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("push after close error = %v", err)
	}
	closedCause := errors.New("shutdown")
	closedContext, cancelClosed := context.WithCancelCause(context.Background())
	cancelClosed(closedCause)
	if err := queue.Push(closedContext, item{}); !errors.Is(err, closedCause) {
		t.Fatalf("canceled push after close error = %v", err)
	}
	value, err := queue.Pop(context.Background())
	if err != nil || value.value != 1 {
		t.Fatalf("closed queue retained value = %#v, %v", value, err)
	}
	if _, err := queue.Pop(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("closed empty queue error = %v", err)
	}
}

func TestQueueDrainDropsEachOwnerOnce(t *testing.T) {
	var drops atomic.Int32
	queue, err := New(Limit{Items: 8}, itemTraits(&drops))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		if err := queue.Push(context.Background(), item{value: index}); err != nil {
			t.Fatal(err)
		}
	}
	queue.Close()
	if got := queue.Drain(); got != 5 || drops.Load() != 5 {
		t.Fatalf("drain = %d, drops = %d", got, drops.Load())
	}
	if queue.Drain() != 0 || drops.Load() != 5 {
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
	if err := queue.Push(context.Background(), item{}); !errors.Is(err, ErrInvalidSize) {
		t.Fatalf("invalid size error = %v", err)
	}
	queue, _ = New(Limit{Items: 1, Time: 1}, Traits[item]{Time: func(item) (int64, bool) { return 0, false }})
	if err := queue.Push(context.Background(), item{}); !errors.Is(err, ErrUnknownTime) {
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
				if err := queue.Push(ctx, item{value: producer*perProducer + index}); err != nil {
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
		value, err := queue.Pop(ctx)
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
	if err := queue.Push(context.Background(), item{value: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.Pop(context.Background()); err != nil {
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
		if err := queue.Push(ctx, 1); err != nil {
			panic(err)
		}
		if _, err := queue.Pop(ctx); err != nil {
			panic(err)
		}
		queue.Complete()
	})
	if allocations != 0 {
		t.Fatalf("queue transfer allocations = %v", allocations)
	}
}
