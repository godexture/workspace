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
	"github.com/godexture/godec/media/schema"
)

type item struct {
	value int
	size  int
	time  int64
}

type queueItemID struct{}
type queueIntID struct{}

// itemType declares the edge payload. The queue takes a schema type because a
// ring slot is an ownership slot: it needs the traits its payloads are owned
// under, not only the measurements a limit uses.
func itemType(traits schema.Traits[item]) schema.Type[item] {
	return schema.Define[queueItemID](traits)
}

func itemTraits() schema.Type[item] {
	return itemType(schema.Traits[item]{
		Size: func(value item) int { return value.size },
		Time: func(value item) (int64, bool) { return value.time, true },
	})
}

// The queue stores cells, so a test hands it one and counts releases through
// the cell rather than through an edge-wide trait.
func pushValue[T any](ctx context.Context, queue *Queue[T], typ schema.Type[T], value T) error {
	var cell flow.Item[T]
	cell.Bind(typ, &testDomain)
	cell.Set(value)
	err := queue.Push(ctx, &cell)
	if err != nil {
		cell.Drop()
	}
	return err
}

func popValue[T any](ctx context.Context, queue *Queue[T], typ schema.Type[T]) (T, error) {
	var cell flow.Item[T]
	cell.Bind(typ, &testDomain)
	defer cell.Drop()
	if err := queue.Pop(ctx, &cell); err != nil {
		var zero T
		return zero, err
	}
	return cell.Value(), nil
}

func TestQueueEnforcesItemsBytesAndTime(t *testing.T) {
	queue, err := New(Limit{Items: 4, Bytes: 7, Span: 10}, itemTraits(), &testDomain)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, value := range []item{{value: 1, size: 3, time: 20}, {value: 2, size: 4, time: 10}} {
		if err := pushValue(ctx, queue, itemTraits(), value); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := queue.Snapshot()
	if snapshot.Items != 2 || snapshot.Bytes != 7 || snapshot.Span != 10 {
		t.Fatalf("queue snapshot = %#v", snapshot)
	}

	pushed := make(chan error, 1)
	go func() { pushed <- pushValue(ctx, queue, itemTraits(), item{value: 3, size: 1, time: 15}) }()
	select {
	case err := <-pushed:
		t.Fatalf("bounded push completed early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	value, err := popValue(ctx, queue, itemTraits())
	if err != nil || value.value != 1 {
		t.Fatalf("pop = %#v, %v", value, err)
	}
	if err := <-pushed; err != nil {
		t.Fatal(err)
	}
	if got := queue.Snapshot(); got.Items != 2 || got.Bytes != 5 || got.Span != 5 {
		t.Fatalf("queue after unblock = %#v", got)
	}
}

func TestQueueWaitsRespectCancellationAndSeal(t *testing.T) {
	queue, err := New(Limit{Items: 1}, itemType(schema.Traits[item]{}), &testDomain)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan error, 1)
	go func() {
		_, err := popValue(ctx, queue, itemTraits())
		wake <- err
	}()
	cancel()
	if err := <-wake; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled pop error = %v", err)
	}
	if err := pushValue(context.Background(), queue, itemTraits(), item{value: 1}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	go func() { wake <- pushValue(ctx, queue, itemTraits(), item{value: 2}) }()
	cancel()
	if err := <-wake; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled push error = %v", err)
	}

	queue.Seal()
	queue.Seal()
	if err := pushValue(context.Background(), queue, itemTraits(), item{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("push after seal error = %v", err)
	}
	closedCause := errors.New("shutdown")
	closedContext, cancelClosed := context.WithCancelCause(context.Background())
	cancelClosed(closedCause)
	if err := pushValue(closedContext, queue, itemTraits(), item{}); !errors.Is(err, closedCause) {
		t.Fatalf("canceled push after seal error = %v", err)
	}
	value, err := popValue(context.Background(), queue, itemTraits())
	if err != nil || value.value != 1 {
		t.Fatalf("sealed queue retained value = %#v, %v", value, err)
	}
	if _, err := popValue(context.Background(), queue, itemTraits()); !errors.Is(err, io.EOF) {
		t.Fatalf("sealed empty queue error = %v", err)
	}
}

func TestQueueAbortIsNotEOFAndPrefersTheCancellationCause(t *testing.T) {
	queue, err := New(Limit{Items: 1}, itemType(schema.Traits[item]{}), &testDomain)
	if err != nil {
		t.Fatal(err)
	}
	if err := pushValue(context.Background(), queue, itemTraits(), item{value: 1}); err != nil {
		t.Fatal(err)
	}
	queue.Abort()
	if _, err := popValue(context.Background(), queue, itemTraits()); !errors.Is(err, ErrAbandoned) || errors.Is(err, io.EOF) {
		t.Fatalf("aborted pop without a cause = %v, want ErrAbandoned and not EOF", err)
	}
	if snapshot := queue.Snapshot(); !snapshot.Closed || snapshot.Sealed || !snapshot.Abandoned {
		t.Fatalf("aborted snapshot = %#v", snapshot)
	}

	cause := errors.New("run stopped")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(cause)
	if _, err := popValue(ctx, queue, itemTraits()); !errors.Is(err, cause) || errors.Is(err, io.EOF) {
		t.Fatalf("aborted pop with a cause = %v, want %v and not EOF", err, cause)
	}
}

func TestQueueSealedPopPrefersAPreCanceledContext(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*Queue[item])
	}{
		{
			name: "empty sealed edge",
		},
		{
			name: "queued value",
			setup: func(queue *Queue[item]) {
				if err := pushValue(context.Background(), queue, itemTraits(), item{value: 1}); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			queue, err := New(Limit{Items: 2}, itemTraits(), &testDomain)
			if err != nil {
				t.Fatal(err)
			}
			if test.setup != nil {
				test.setup(queue)
			}
			queue.Seal()
			ctx, cancel := context.WithCancelCause(context.Background())
			want := errors.New("canceled after seal")
			cancel(want)
			if _, err := popValue(ctx, queue, itemTraits()); !errors.Is(err, want) || errors.Is(err, io.EOF) {
				t.Fatalf("sealed Pop = %v, want cancellation cause (not EOF)", err)
			}
			queue.Drain()
		})
	}
}

func TestQueueDrainDropsEachOwnerOnce(t *testing.T) {
	var drops atomic.Int32
	counting := itemType(schema.Traits[item]{Drop: func(item) { drops.Add(1) }})
	queue, err := New(Limit{Items: 8}, counting, &testDomain)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		if err := pushValue(context.Background(), queue, counting, item{value: index}); err != nil {
			t.Fatal(err)
		}
	}
	queue.Seal()
	if got := queue.Drain(); got != 5 || drops.Load() != 5 {
		t.Fatalf("drain = %d, drops = %d", got, drops.Load())
	}
	if again := queue.Drain(); again != 0 || drops.Load() != 5 {
		t.Fatal("repeated drain released an owner twice")
	}
}

func TestQueueRejectsUnavailableOrInvalidLimitTraits(t *testing.T) {
	if _, err := New(Limit{}, itemType(schema.Traits[item]{}), &testDomain); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("invalid limit error = %v", err)
	}
	if _, err := New(Limit{Items: 1, Bytes: 1}, itemType(schema.Traits[item]{}), &testDomain); !errors.Is(err, ErrSizeTrait) {
		t.Fatalf("missing size trait error = %v", err)
	}
	if _, err := New(Limit{Items: 1, Span: 1}, itemType(schema.Traits[item]{}), &testDomain); !errors.Is(err, ErrTimeTrait) {
		t.Fatalf("missing time trait error = %v", err)
	}
	queue, _ := New(Limit{Items: 1, Bytes: 1}, itemType(schema.Traits[item]{Size: func(item) int { return -1 }}), &testDomain)
	if err := pushValue(context.Background(), queue, itemTraits(), item{}); !errors.Is(err, ErrInvalidSize) {
		t.Fatalf("invalid size error = %v", err)
	}
	queue, _ = New(Limit{Items: 1, Span: 1}, itemType(schema.Traits[item]{Time: func(item) (int64, bool) { return 0, false }}), &testDomain)
	if err := pushValue(context.Background(), queue, itemTraits(), item{}); !errors.Is(err, ErrUnknownTime) {
		t.Fatalf("unknown time error = %v", err)
	}
}

func TestQueueConcurrentProducersDoNotLoseOrDuplicateItems(t *testing.T) {
	const producers = 4
	const perProducer = 200
	queue, err := New(Limit{Items: 17}, itemType(schema.Traits[item]{}), &testDomain)
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
				if err := pushValue(ctx, queue, itemTraits(), item{value: producer*perProducer + index}); err != nil {
					t.Errorf("push: %v", err)
					return
				}
			}
		}(producer)
	}
	go func() {
		producersDone.Wait()
		queue.Seal()
	}()
	seen := make([]bool, producers*perProducer)
	for {
		value, err := popValue(ctx, queue, itemTraits())
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
	queue, err := New(Limit{Items: 1}, itemType(schema.Traits[item]{}), &testDomain)
	if err != nil {
		t.Fatal(err)
	}
	if err := pushValue(context.Background(), queue, itemTraits(), item{value: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := popValue(context.Background(), queue, itemTraits()); err != nil {
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
	typ := schema.Define[queueIntID](schema.Traits[int]{})
	queue, err := New(Limit{Items: 1}, typ, &testDomain)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	allocations := testing.AllocsPerRun(1000, func() {
		if err := pushValue(ctx, queue, typ, 1); err != nil {
			panic(err)
		}
		if _, err := popValue(ctx, queue, typ); err != nil {
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
	queue, err := New(Limit{Items: 4}, itemType(schema.Traits[item]{}), &testDomain)
	if err != nil {
		t.Fatal(err)
	}
	if err := pushValue(context.Background(), queue, itemTraits(), item{value: 1}); err != nil {
		t.Fatal(err)
	}
	var domain flow.Collector
	var into flow.Item[item]
	into.Bind(itemType(schema.Traits[item]{Drop: func(item) { panic("declared drop panicked") }}), &domain)
	into.Set(item{value: 2})
	if err := queue.Pop(context.Background(), &into); err != nil {
		t.Fatalf("pop over a failing release = %v", err)
	}
	if len(domain.Failures()) != 1 {
		t.Fatalf("failures reported to the slot's domain = %d, want the release Pop performed", len(domain.Failures()))
	}
	if !queue.mu.TryLock() {
		t.Fatal("the queue kept its lock across a declared drop")
	}
	queue.mu.Unlock()
	queue.Drain()
}

// Drain releases owners, so one that cannot be released must not strand the
// ones behind it. Every owner is released and the failures reach the edge's own
// domain, because Drain runs where no recovery boundary is left.
func TestQueueDrainReleasesEveryOwnerDespiteAFailingDrop(t *testing.T) {
	var released atomic.Int32
	failing := itemType(schema.Traits[item]{
		Drop: func(value item) {
			released.Add(1)
			if value.value == 0 {
				panic("declared drop panicked")
			}
		},
	})
	var domain flow.Collector
	queue, err := New(Limit{Items: 4}, failing, &domain)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if err := pushValue(context.Background(), queue, failing, item{value: index}); err != nil {
			t.Fatal(err)
		}
	}
	if dropped := queue.Drain(); dropped != 3 {
		t.Fatalf("drain = %d, want 3", dropped)
	}
	if released.Load() != 3 {
		t.Fatalf("released owners = %d, want every owner released", released.Load())
	}
	if got := len(domain.Failures()); got != 1 {
		t.Fatalf("failures reported to the edge's own domain = %d, want the one release that could not finish", got)
	}
}
