// Package queue implements typed bounded runtime edges. It is internal so
// plugin contracts never expose a queue implementation or scheduling type.
package queue

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/release"
	"github.com/godexture/godec/media/schema"
)

var (
	ErrInvalidLimit = errors.New("runtime queue requires a positive item limit and non-negative byte/time limits")
	ErrSizeTrait    = errors.New("runtime queue byte limit requires a size trait")
	ErrTimeTrait    = errors.New("runtime queue time limit requires a timestamp trait")
	ErrInvalidSize  = errors.New("runtime queue size trait returned an invalid size")
	ErrUnknownTime  = errors.New("runtime queue time trait returned an unknown timestamp")
	ErrClosed       = errors.New("runtime queue is closed")
	ErrInvalidItem  = errors.New("runtime queue requires an owned item cell")
)

// Limit is enforced locally by one edge. Time is expressed in the connected
// stream descriptor's ticks; conversion into a common time base is a planner
// decision and never occurs in the item loop.
type Limit struct {
	Items int
	Bytes int64
	Time  int64
}

func (l Limit) Valid() bool { return l.Items > 0 && l.Bytes >= 0 && l.Time >= 0 }

type entry[T any] struct {
	cell flow.Item[T]
	size int64
	time int64
}

// Queue is a bounded ring with context-aware waits and idempotent close. It
// owns every successfully pushed cell until Pop succeeds. Drain releases all
// remaining owners through the cells themselves.
type Queue[T any] struct {
	mu        sync.Mutex
	notEmpty  chan struct{}
	notFull   chan struct{}
	idle      chan struct{}
	limit     Limit
	typ       schema.Type[T]
	size      func(T) int
	time      func(T) (int64, bool)
	values    []entry[T]
	head      int
	count     int
	active    int
	bytes     int64
	minTime   int64
	maxTime   int64
	closed    bool
	abandoned bool
}

func New[T any](limit Limit, typ schema.Type[T]) (*Queue[T], error) {
	if !limit.Valid() {
		return nil, ErrInvalidLimit
	}
	traits := typ.Traits()
	if limit.Bytes != 0 && traits.Size == nil {
		return nil, ErrSizeTrait
	}
	if limit.Time != 0 && traits.Time == nil {
		return nil, ErrTimeTrait
	}
	return &Queue[T]{
		notEmpty: make(chan struct{}, 1),
		notFull:  make(chan struct{}, 1),
		idle:     make(chan struct{}, 1),
		limit:    limit,
		typ:      typ,
		size:     traits.Size,
		time:     traits.Time,
		values:   make([]entry[T], limit.Items),
	}, nil
}

// Bind puts the ring's slots in the failure domain of the task that drains this
// edge. A payload pushed into the ring stops being the producer's, so a release
// it cannot perform is answered for by the consumer that owns it now.
//
// The slots are all empty until the first push, which is what makes this the
// only rebinding the ring ever needs.
func (q *Queue[T]) Bind(reporter flow.Reporter) {
	if q == nil {
		return
	}
	q.mu.Lock()
	for index := range q.values {
		q.values[index].cell.Bind(q.typ, reporter)
	}
	q.mu.Unlock()
}

// Push moves the cell's payload into the ring. The cell is emptied only when
// the ring accepted it, so a rejected or cancelled push leaves the producer
// still holding its item and no payload exists in two places.
func (q *Queue[T]) Push(ctx context.Context, item *flow.Item[T]) error {
	if q == nil {
		return ErrClosed
	}
	if item == nil || !item.Valid() {
		return ErrInvalidItem
	}
	size, itemTime, err := q.measure(item.Value())
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		q.mu.Lock()
		if q.closed {
			q.mu.Unlock()
			if cause := context.Cause(ctx); cause != nil {
				return cause
			}
			return ErrClosed
		}
		if q.fits(size, itemTime) {
			q.push(item, size, itemTime)
			q.notify(q.notEmpty)
			q.mu.Unlock()
			return nil
		}
		notFull := q.notFull
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-notFull:
		}
	}
}

// Pop moves the oldest queued payload into into.
//
// Anything into still held is released first and outside the lock. A declared
// Drop is third-party code, and running it while holding the ring's mutex
// would leave that mutex locked forever if it panicked: the deferred Drain
// that would otherwise clean up waits on the same mutex, so the panic would
// never reach a recovery boundary.
func (q *Queue[T]) Pop(ctx context.Context, into *flow.Item[T]) error {
	if q == nil {
		return io.EOF
	}
	if into == nil {
		return ErrInvalidItem
	}
	into.Drop()
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		q.mu.Lock()
		if q.count != 0 {
			q.pop(into)
			q.active++
			if !q.closed {
				q.notify(q.notFull)
			}
			q.mu.Unlock()
			return nil
		}
		if q.closed {
			q.mu.Unlock()
			return io.EOF
		}
		notEmpty := q.notEmpty
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-notEmpty:
		}
	}
}

// Complete acknowledges that the consumer has finished processing one value
// returned by Pop. It lets the runtime establish a quiescent barrier without
// closing the queue or placing control sentinels in the media stream.
//
// Abandon is the same bookkeeping for a value the consumer never finished. The
// pair only says something to an edge whose barrier is WaitIdle: one lets that
// barrier report quiescence and the other stops it. An edge that establishes
// quiescence some other way returns its slots with Complete, because there the
// call is only capacity.
func (q *Queue[T]) Complete() {
	if q == nil {
		return
	}
	q.mu.Lock()
	if q.active > 0 {
		q.active--
		if q.count == 0 && q.active == 0 {
			q.notify(q.idle)
		}
	}
	q.mu.Unlock()
}

// Abandon settles one value Pop returned whose consumer stopped before
// completing it. The active count becomes accurate again, because nothing is
// being processed any more.
//
// The edge cannot become quiescent afterwards: that value never finished
// downstream, and a barrier reporting idle would claim work that did not
// happen, which is what lets a caller move on to Finalize and Flush over a
// data path that has already died. Only a failing consumer abandons a value,
// and its failure cancels the barrier's context, so the wait ends with that
// failure rather than with this bookkeeping.
func (q *Queue[T]) Abandon() {
	if q == nil {
		return
	}
	q.mu.Lock()
	if q.active > 0 {
		q.active--
		q.abandoned = true
	}
	q.mu.Unlock()
}

// WaitIdle waits until every value pushed before the call has both left the
// ring and completed downstream processing. Producers must already be
// quiescent when this barrier is used.
func (q *Queue[T]) WaitIdle(ctx context.Context) error {
	if q == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		q.mu.Lock()
		if q.count == 0 && q.active == 0 && !q.abandoned {
			q.notify(q.idle)
			q.mu.Unlock()
			return nil
		}
		idle := q.idle
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-idle:
		}
	}
}

// Close marks the input edge closed and wakes every blocked reader/writer. It
// does not discard queued values; a consumer may drain them before Pop reports
// EOF, or the edge owner may call Drain during cancellation/rollback.
func (q *Queue[T]) Close() {
	if q == nil {
		return
	}
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.notEmpty)
		close(q.notFull)
	}
	q.mu.Unlock()
}

// Drain releases all queued owners into the domain that is doing the draining
// and returns their count. That domain is the edge's task while it runs, and
// the caller's cleanup once it has joined, which is why it is named here rather
// than taken from the ring.
//
// It takes the whole ring out under the lock and releases the slots after
// letting it go, so a declared Drop never runs while the mutex is held. It is
// safe to call repeatedly; callers normally close or cancel producers first.
func (q *Queue[T]) Drain(into flow.Reporter) int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	queued := make([]flow.Item[T], q.count)
	for index := range queued {
		queued[index].Bind(q.typ, into)
		q.pop(&queued[index])
	}
	if !q.closed {
		q.notify(q.notFull)
	}
	if q.count == 0 && q.active == 0 {
		q.notify(q.idle)
	}
	q.mu.Unlock()
	release.All(queued)
	return len(queued)
}

type Snapshot struct {
	Items  int
	Active int
	Bytes  int64
	Time   int64
	Closed bool
}

// Snapshot reads edge-local resource state. Runtime calls it only during an
// explicit observation snapshot or after tasks have joined.
func (q *Queue[T]) Snapshot() Snapshot {
	if q == nil {
		return Snapshot{Closed: true}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return Snapshot{Items: q.count, Active: q.active, Bytes: q.bytes, Time: q.span(), Closed: q.closed}
}

func (q *Queue[T]) measure(value T) (size, valueTime int64, err error) {
	if q.limit.Bytes != 0 {
		measured := q.size(value)
		if measured < 0 {
			return 0, 0, ErrInvalidSize
		}
		size = int64(measured)
		if size > q.limit.Bytes {
			return 0, 0, ErrInvalidSize
		}
	}
	if q.limit.Time != 0 {
		measured, ok := q.time(value)
		if !ok {
			return 0, 0, ErrUnknownTime
		}
		valueTime = measured
	}
	return size, valueTime, nil
}

func (q *Queue[T]) fits(size, itemTime int64) bool {
	if q.count == q.limit.Items || q.limit.Bytes != 0 && size > q.limit.Bytes-q.bytes {
		return false
	}
	if q.limit.Time == 0 || q.count == 0 {
		return true
	}
	minimum, maximum := q.minTime, q.maxTime
	if itemTime < minimum {
		minimum = itemTime
	}
	if itemTime > maximum {
		maximum = itemTime
	}
	return uint64(maximum)-uint64(minimum) <= uint64(q.limit.Time)
}

func (q *Queue[T]) push(item *flow.Item[T], size, itemTime int64) {
	slot := &q.values[(q.head+q.count)%len(q.values)]
	slot.cell.Move(item)
	slot.size, slot.time = size, itemTime
	q.count++
	q.bytes += size
	if q.count == 1 {
		q.minTime, q.maxTime = itemTime, itemTime
	} else if q.limit.Time != 0 {
		if itemTime < q.minTime {
			q.minTime = itemTime
		}
		if itemTime > q.maxTime {
			q.maxTime = itemTime
		}
	}
}

func (q *Queue[T]) pop(into *flow.Item[T]) {
	slot := &q.values[q.head]
	into.Move(&slot.cell)
	size, slotTime := slot.size, slot.time
	slot.size, slot.time = 0, 0
	q.head = (q.head + 1) % len(q.values)
	q.count--
	q.bytes -= size
	if q.count == 0 {
		q.minTime, q.maxTime = 0, 0
	} else if q.limit.Time != 0 && (slotTime == q.minTime || slotTime == q.maxTime) {
		q.recomputeTime()
	}
}

func (q *Queue[T]) recomputeTime() {
	first := q.values[q.head].time
	minimum, maximum := first, first
	for offset := 1; offset < q.count; offset++ {
		value := q.values[(q.head+offset)%len(q.values)].time
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
	}
	q.minTime, q.maxTime = minimum, maximum
}

func (q *Queue[T]) span() int64 {
	if q.limit.Time == 0 || q.count < 2 {
		return 0
	}
	return q.maxTime - q.minTime
}

func (q *Queue[T]) notify(condition chan struct{}) {
	select {
	case condition <- struct{}{}:
	default:
	}
}
