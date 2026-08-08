// Package observe collects island-local measurements into immutable job
// snapshots. Off mode allocates no local counter and never reads a clock.
package observe

import (
	"sync"
	"time"
)

type Mode uint8

const (
	Off Mode = iota
	Basic
	Detailed
	Trace
)

func (m Mode) Valid() bool { return m <= Trace }

type Kind uint8

const (
	Progress Kind = iota + 1
	Lifecycle
	Diagnostic
)

func (k Kind) Valid() bool { return k >= Progress && k <= Diagnostic }

// Event is the single internal observation record. Zero At and byte/time
// fields are intentional at lower observation levels.
type Event struct {
	Sequence uint64
	Kind     Kind
	Node     string
	Edge     string
	Phase    string
	Code     string
	Message  string
	Items    uint64
	Bytes    uint64
	Media    int64
	HasMedia bool
	At       time.Time
	Detail   map[string]string
}

func (e Event) clone() Event {
	e.Detail = cloneMap(e.Detail)
	return e
}

type Clock func() time.Time

type Collector struct {
	mode  Mode
	clock Clock

	mu     sync.Mutex
	next   uint64
	events []Event
}

func New(mode Mode, clock Clock) *Collector {
	if !mode.Valid() {
		mode = Off
	}
	if clock == nil {
		clock = time.Now
	}
	return &Collector{mode: mode, clock: clock}
}

func (c *Collector) Mode() Mode {
	if c == nil {
		return Off
	}
	return c.mode
}

// Emit records a control-plane event. Off suppresses optional observation;
// correctness diagnostics are retained separately by Host Result.
func (c *Collector) Emit(event Event) {
	if c == nil || c.mode == Off || !event.Kind.Valid() {
		return
	}
	if c.mode >= Detailed && event.At.IsZero() {
		event.At = c.clock()
	}
	c.append(event)
}

func (c *Collector) append(event Event) {
	c.mu.Lock()
	event.Sequence = c.next
	c.next++
	c.events = append(c.events, event.clone())
	c.mu.Unlock()
}

func (c *Collector) Snapshot() []Event {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Event, len(c.events))
	for index, event := range c.events {
		result[index] = event.clone()
	}
	return result
}

// Local is owned by one island/task and therefore needs no atomic operations.
type Local struct {
	collector *Collector
	node      string
	edge      string
	items     uint64
	bytes     uint64
	media     int64
	hasMedia  bool
	started   time.Time
}

func (l *Local) Detailed() bool { return l != nil && l.collector.mode >= Detailed }

func (c *Collector) Local(node, edge string) *Local {
	if c == nil || c.mode == Off {
		return nil
	}
	local := &Local{collector: c, node: node, edge: edge}
	if c.mode >= Detailed {
		local.started = c.clock()
	}
	return local
}

// Add performs only plain task-local arithmetic. Callers pass byte/media
// values only for Detailed/Trace strategies, so Basic never evaluates schema
// Size/Time traits.
func (l *Local) Add(bytes uint64, media int64, hasMedia bool) {
	if l == nil {
		return
	}
	l.items++
	if l.collector.mode >= Detailed {
		l.bytes += bytes
		l.media = media
		l.hasMedia = hasMedia
	}
	if l.collector.mode == Trace {
		l.collector.Emit(Event{Kind: Progress, Node: l.node, Edge: l.edge, Items: 1, Bytes: bytes, Media: media, HasMedia: hasMedia})
	}
}

// Flush publishes one batched progress event. Trace already emitted per-item
// events and only emits a final aggregate when at least one item was seen.
func (l *Local) Flush() {
	if l == nil || l.items == 0 {
		return
	}
	event := Event{Kind: Progress, Node: l.node, Edge: l.edge, Items: l.items}
	if l.collector.mode >= Detailed {
		event.Bytes = l.bytes
		event.Media = l.media
		event.HasMedia = l.hasMedia
		event.At = l.collector.clock()
	}
	l.collector.append(event)
	l.items = 0
	l.bytes = 0
	l.media = 0
	l.hasMedia = false
}

func cloneMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
