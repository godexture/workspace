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
	ErrInvalidLimit = errors.New("runtime queue requires a positive item limit and non-negative byte/span limits")
	ErrSizeTrait    = errors.New("runtime queue byte limit requires a size trait")
	ErrTimeTrait    = errors.New("runtime queue span limit requires a timestamp trait")
	ErrInvalidSize  = errors.New("runtime queue size trait returned an invalid size")
	ErrUnknownTime  = errors.New("runtime queue time trait returned an unknown timestamp")
	ErrClosed       = errors.New("runtime queue is closed")
	ErrInvalidItem  = errors.New("runtime queue requires an owned item cell")
	ErrDomain       = errors.New("runtime queue requires the failure domain of the task that drains it")
	ErrAbandoned    = errors.New("runtime queue was abandoned")
)

type terminal uint8

const (
	accepting terminal = iota
	sealed
	abandoned
)

// Limit is enforced locally by one edge. Span is expressed in the connected
// stream descriptor's ticks; conversion into a common time base is a planner
// decision and never occurs in the item loop.
type Limit struct {
	Items int
	Bytes int64
	Span  int64
}

func (l Limit) Valid() bool { return l.Items > 0 && l.Bytes >= 0 && l.Span >= 0 }

type entry[T any] struct {
	cell flow.Item[T]
	size int64
	time int64
}

// Queue is a bounded ring with context-aware waits and idempotent close. It
// owns every successfully pushed cell until Pop succeeds. Drain releases all
// remaining owners through the cells themselves.
type Queue[T any] struct {
	mu         sync.Mutex
	notEmpty   chan struct{}
	notFull    chan struct{}
	idle       chan struct{}
	limit      Limit
	typ        schema.Type[T]
	into       flow.Reporter
	size       func(T) int
	time       func(T) (int64, bool)
	values     []entry[T]
	head       int
	count      int
	active     int
	bytes      int64
	minTime    int64
	maxTime    int64
	terminal   terminal
	unfinished bool
}

// New opens a bounded edge in the failure domain of the task that drains it. A
// payload pushed into the ring stops being the producer's, so a release the
// ring cannot perform is answered for by the consumer that owns it now.
//
// The domain is a construction argument rather than a later binding call
// because a ring whose slots are unbound cannot accept a payload at all: the
// first producer to reach it would be refused, and the edge would fail for a
// reason that has nothing to do with the edge.
func New[T any](limit Limit, typ schema.Type[T], into flow.Reporter) (*Queue[T], error) {
	if !limit.Valid() {
		return nil, ErrInvalidLimit
	}
	if into == nil {
		return nil, ErrDomain
	}
	traits := typ.Traits()
	if limit.Bytes != 0 && traits.Size == nil {
		return nil, ErrSizeTrait
	}
	if limit.Span != 0 && traits.Time == nil {
		return nil, ErrTimeTrait
	}
	result := &Queue[T]{
		notEmpty: make(chan struct{}, 1),
		notFull:  make(chan struct{}, 1),
		idle:     make(chan struct{}, 1),
		limit:    limit,
		typ:      typ,
		into:     into,
		size:     traits.Size,
		time:     traits.Time,
		values:   make([]entry[T], limit.Items),
	}
	for index := range result.values {
		result.values[index].cell.Bind(typ, into)
	}
	return result, nil
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
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		q.mu.Lock()
		// Recheck after taking the lock. Cancellation may race a producer
		// waking on a terminal transition or a newly available slot; the
		// context cause wins over every queue result, including sealed EOF.
		if cause := context.Cause(ctx); cause != nil {
			q.mu.Unlock()
			return cause
		}
		switch q.terminal {
		case abandoned:
			q.mu.Unlock()
			return q.stopped(ctx)
		case sealed:
			q.mu.Unlock()
			return ErrClosed
		}
		if q.fits(size, itemTime) {
			pushed := q.push(item, size, itemTime)
			if pushed {
				q.notify(q.notEmpty)
			}
			q.mu.Unlock()
			if !pushed {
				return ErrInvalidItem
			}
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
	if !into.Bound() {
		return ErrInvalidItem
	}
	into.Drop()
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		q.mu.Lock()
		// A cancellation that raced the wake-up must not be turned into a
		// successful delivery or graceful EOF. Check under the queue lock so a
		// sealed edge cannot outrun the context and start downstream Flush.
		if cause := context.Cause(ctx); cause != nil {
			q.mu.Unlock()
			return cause
		}
		if q.terminal == abandoned {
			q.mu.Unlock()
			return q.stopped(ctx)
		}
		if q.terminal == sealed {
			if q.count != 0 {
				q.pop(into)
				q.active++
				q.mu.Unlock()
				return nil
			}
			q.mu.Unlock()
			return io.EOF
		}
		if q.count != 0 {
			q.pop(into)
			q.active++
			q.notify(q.notFull)
			q.mu.Unlock()
			return nil
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
		q.unfinished = true
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
		if q.count == 0 && q.active == 0 && !q.unfinished {
			q.notify(q.idle)
			q.mu.Unlock()
			return nil
		}
		if cause := context.Cause(ctx); cause != nil {
			q.mu.Unlock()
			return cause
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

// Seal marks a producer-complete edge and wakes every blocked reader/writer.
// It does not discard queued values; a consumer drains them before Pop reports
// EOF. Only a seal represents ordinary end-of-stream.
func (q *Queue[T]) Seal() {
	if q == nil {
		return
	}
	q.mu.Lock()
	if q.terminal == accepting {
		q.terminal = sealed
		close(q.notEmpty)
		close(q.notFull)
	}
	q.mu.Unlock()
}

// Abort stops an accepting edge without declaring end-of-stream. A sealed
// edge keeps its already committed EOF. A Pop on an aborted edge returns its
// context cause when one exists; otherwise it returns the internal
// ErrAbandoned control value. The latter lets a deliberate downstream early
// stop settle upstream tasks without inventing a second work failure.
func (q *Queue[T]) Abort() {
	if q == nil {
		return
	}
	q.mu.Lock()
	wake := q.terminal == accepting
	if wake {
		q.terminal = abandoned
	}
	if wake {
		close(q.notEmpty)
		close(q.notFull)
	}
	q.mu.Unlock()
}

// Drain releases all queued owners and returns their count.
//
// They are released in the edge's own domain, which is where they have
// belonged since they were pushed, whether the edge's task is still running or
// has already joined. The lifecycle step a failed release lands under comes
// from the operation that domain is performing at the time, so a discard after
// the join is recorded as a discard without the payload changing hands.
//
// It takes the whole ring out under the lock and releases the slots after
// letting it go, so a declared Drop never runs while the mutex is held. It is
// safe to call repeatedly; callers normally close or cancel producers first.
func (q *Queue[T]) Drain() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	queued := make([]flow.Item[T], q.count)
	for index := range queued {
		queued[index].Bind(q.typ, q.into)
		q.pop(&queued[index])
	}
	if q.terminal == accepting {
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
	Items     int
	Active    int
	Bytes     int64
	Span      int64
	Closed    bool
	Sealed    bool
	Abandoned bool
}

// Snapshot reads edge-local resource state. Runtime calls it only during an
// explicit observation snapshot or after tasks have joined.
func (q *Queue[T]) Snapshot() Snapshot {
	if q == nil {
		return Snapshot{Closed: true}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return Snapshot{
		Items:     q.count,
		Active:    q.active,
		Bytes:     q.bytes,
		Span:      q.span(),
		Closed:    q.terminal != accepting,
		Sealed:    q.terminal == sealed,
		Abandoned: q.terminal == abandoned,
	}
}

func (q *Queue[T]) stopped(ctx context.Context) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ErrAbandoned
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
	if q.limit.Span != 0 {
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
	if q.limit.Span == 0 || q.count == 0 {
		return true
	}
	minimum, maximum := q.minTime, q.maxTime
	if itemTime < minimum {
		minimum = itemTime
	}
	if itemTime > maximum {
		maximum = itemTime
	}
	return uint64(maximum)-uint64(minimum) <= uint64(q.limit.Span)
}

func (q *Queue[T]) push(item *flow.Item[T], size, itemTime int64) bool {
	slot := &q.values[(q.head+q.count)%len(q.values)]
	if !slot.cell.Move(item) {
		return false
	}
	slot.size, slot.time = size, itemTime
	q.count++
	q.bytes += size
	if q.count == 1 {
		q.minTime, q.maxTime = itemTime, itemTime
	} else if q.limit.Span != 0 {
		if itemTime < q.minTime {
			q.minTime = itemTime
		}
		if itemTime > q.maxTime {
			q.maxTime = itemTime
		}
	}
	return true
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
	} else if q.limit.Span != 0 && (slotTime == q.minTime || slotTime == q.maxTime) {
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
	if q.limit.Span == 0 || q.count < 2 {
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
