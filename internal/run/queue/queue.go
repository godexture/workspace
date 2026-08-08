// Package queue implements typed bounded runtime edges. It is internal so
// plugin contracts never expose a queue implementation or scheduling type.
package queue

import (
	"context"
	"errors"
	"io"
	"sync"
)

var (
	ErrInvalidLimit = errors.New("runtime queue requires a positive item limit and non-negative byte/time limits")
	ErrSizeTrait    = errors.New("runtime queue byte limit requires a size trait")
	ErrTimeTrait    = errors.New("runtime queue time limit requires a timestamp trait")
	ErrInvalidSize  = errors.New("runtime queue size trait returned an invalid size")
	ErrUnknownTime  = errors.New("runtime queue time trait returned an unknown timestamp")
	ErrClosed       = errors.New("runtime queue is closed")
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

type Traits[T any] struct {
	Drop func(T)
	Size func(T) int
	Time func(T) (int64, bool)
}

type entry[T any] struct {
	value T
	size  int64
	time  int64
}

// Queue is a bounded ring with context-aware waits and idempotent close. It
// owns every successfully pushed value until Pop succeeds. Drain releases all
// remaining owners through the captured Drop trait.
type Queue[T any] struct {
	mu       sync.Mutex
	notEmpty chan struct{}
	notFull  chan struct{}
	idle     chan struct{}
	limit    Limit
	traits   Traits[T]
	values   []entry[T]
	head     int
	count    int
	active   int
	bytes    int64
	minTime  int64
	maxTime  int64
	closed   bool
}

func New[T any](limit Limit, traits Traits[T]) (*Queue[T], error) {
	if !limit.Valid() {
		return nil, ErrInvalidLimit
	}
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
		traits:   traits,
		values:   make([]entry[T], limit.Items),
	}, nil
}

func (q *Queue[T]) Push(ctx context.Context, value T) error {
	if q == nil {
		return ErrClosed
	}
	item, err := q.describe(value)
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
		if q.fits(item) {
			q.push(item)
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

func (q *Queue[T]) Pop(ctx context.Context) (T, error) {
	var zero T
	if q == nil {
		return zero, io.EOF
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		q.mu.Lock()
		if q.count != 0 {
			item := q.pop()
			q.active++
			if !q.closed {
				q.notify(q.notFull)
			}
			q.mu.Unlock()
			return item.value, nil
		}
		if q.closed {
			q.mu.Unlock()
			return zero, io.EOF
		}
		notEmpty := q.notEmpty
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return zero, context.Cause(ctx)
		case <-notEmpty:
		}
	}
}

// Complete acknowledges that the consumer has finished processing one value
// returned by Pop. It lets the runtime establish a quiescent barrier without
// closing the queue or placing control sentinels in the media stream.
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
		if q.count == 0 && q.active == 0 {
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

// Drain drops all queued owners and returns their count. It is safe to call
// repeatedly; callers normally close/cancel producers before draining.
func (q *Queue[T]) Drain() int {
	if q == nil {
		return 0
	}
	dropped := 0
	for {
		q.mu.Lock()
		if q.count == 0 {
			q.mu.Unlock()
			return dropped
		}
		item := q.pop()
		if !q.closed {
			q.notify(q.notFull)
		}
		if q.count == 0 && q.active == 0 {
			q.notify(q.idle)
		}
		q.mu.Unlock()
		if q.traits.Drop != nil {
			q.traits.Drop(item.value)
		}
		dropped++
	}
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

func (q *Queue[T]) describe(value T) (entry[T], error) {
	item := entry[T]{value: value}
	if q.limit.Bytes != 0 {
		size := q.traits.Size(value)
		if size < 0 {
			return entry[T]{}, ErrInvalidSize
		}
		item.size = int64(size)
		if item.size > q.limit.Bytes {
			return entry[T]{}, ErrInvalidSize
		}
	}
	if q.limit.Time != 0 {
		valueTime, ok := q.traits.Time(value)
		if !ok {
			return entry[T]{}, ErrUnknownTime
		}
		item.time = valueTime
	}
	return item, nil
}

func (q *Queue[T]) fits(item entry[T]) bool {
	if q.count == q.limit.Items || q.limit.Bytes != 0 && item.size > q.limit.Bytes-q.bytes {
		return false
	}
	if q.limit.Time == 0 || q.count == 0 {
		return true
	}
	minimum, maximum := q.minTime, q.maxTime
	if item.time < minimum {
		minimum = item.time
	}
	if item.time > maximum {
		maximum = item.time
	}
	return uint64(maximum)-uint64(minimum) <= uint64(q.limit.Time)
}

func (q *Queue[T]) push(item entry[T]) {
	index := (q.head + q.count) % len(q.values)
	q.values[index] = item
	q.count++
	q.bytes += item.size
	if q.count == 1 {
		q.minTime, q.maxTime = item.time, item.time
	} else if q.limit.Time != 0 {
		if item.time < q.minTime {
			q.minTime = item.time
		}
		if item.time > q.maxTime {
			q.maxTime = item.time
		}
	}
}

func (q *Queue[T]) pop() entry[T] {
	item := q.values[q.head]
	var zero entry[T]
	q.values[q.head] = zero
	q.head = (q.head + 1) % len(q.values)
	q.count--
	q.bytes -= item.size
	if q.count == 0 {
		q.minTime, q.maxTime = 0, 0
	} else if q.limit.Time != 0 && (item.time == q.minTime || item.time == q.maxTime) {
		q.recomputeTime()
	}
	return item
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
