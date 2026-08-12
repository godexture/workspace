package observe

import (
	"context"
	"sync"
	"time"
)

type Collector struct {
	mode  Mode
	clock Clock
	fail  func(error)

	mu              sync.Mutex
	next            uint64
	history         []Event
	historyStart    int
	historyCount    int
	historyDropped  uint64
	delivery        chan Event
	deliveryDropped uint64
	deliveryFailed  bool
	err             error
	closed          bool

	ctx         context.Context
	cancel      context.CancelCauseFunc
	done        chan struct{}
	closeOnce   sync.Once
	failureOnce sync.Once
}

func New(mode Mode, config Config, clock Clock) *Collector {
	if !mode.Valid() || mode == Off || config.HistoryLimit <= 0 && (config.DeliveryLimit <= 0 || config.Sink == nil) {
		return nil
	}
	if clock == nil {
		clock = time.Now
	}
	collector := &Collector{mode: mode, clock: clock, fail: config.Fail}
	if config.HistoryLimit > 0 {
		collector.history = make([]Event, config.HistoryLimit)
	}
	if config.DeliveryLimit > 0 && config.Sink != nil {
		parent := config.Context
		if parent == nil {
			parent = context.Background()
		}
		collector.ctx, collector.cancel = context.WithCancelCause(parent)
		collector.delivery = make(chan Event, config.DeliveryLimit)
		collector.done = make(chan struct{})
		go collector.dispatch(config.Sink)
	}
	return collector
}

func (c *Collector) Mode() Mode {
	if c == nil {
		return Off
	}
	return c.mode
}

// Emit records and schedules one event without waiting for a live sink.
func (c *Collector) Emit(event Event) {
	if c == nil || !event.Kind.Valid() {
		return
	}
	if c.mode >= Detailed && event.At.IsZero() {
		event.At = c.clock()
	}
	c.append(event)
}

func (c *Collector) append(event Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	event.Sequence = c.next
	c.next++
	if len(c.history) != 0 {
		index := (c.historyStart + c.historyCount) % len(c.history)
		if c.historyCount == len(c.history) {
			index = c.historyStart
			c.historyStart = (c.historyStart + 1) % len(c.history)
			c.historyDropped++
		} else {
			c.historyCount++
		}
		c.history[index] = event.clone()
	}
	if c.delivery == nil {
		return
	}
	if c.deliveryFailed {
		c.deliveryDropped++
		return
	}
	select {
	case c.delivery <- event.clone():
	default:
		c.deliveryDropped++
	}
}

func (c *Collector) Snapshot() []Event {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Event, c.historyCount)
	for index := range result {
		result[index] = c.history[(c.historyStart+index)%len(c.history)].clone()
	}
	return result
}

func (c *Collector) Summary() Summary {
	if c == nil {
		return Summary{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return Summary{HistoryDropped: c.historyDropped, DeliveryDropped: c.deliveryDropped}
}

func (c *Collector) Err() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close stops accepting events and waits for queued delivery until ctx ends.
func (c *Collector) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		if c.delivery != nil {
			close(c.delivery)
		}
		c.mu.Unlock()
	})
	if c.done == nil {
		return c.Err()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-c.done:
		return c.Err()
	case <-ctx.Done():
		c.cancel(context.Cause(ctx))
		return ctx.Err()
	}
}
