package observe

import "time"

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
	if c == nil {
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
