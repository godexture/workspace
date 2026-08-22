package plan

import (
	"sort"
	"time"

	"github.com/godexture/godec/flow"
)

// BufferReason is a bit set because one edge can separate both source and
// sink I/O or combine a fan boundary with an I/O boundary.
type BufferReason uint8

const (
	SourceBuffer BufferReason = 1 << iota
	SinkBuffer
	FanOutBuffer
	FanInBuffer
	ExplicitBuffer
)

const knownBufferReasons = SourceBuffer | SinkBuffer | FanOutBuffer | FanInBuffer | ExplicitBuffer

func (r BufferReason) Valid() bool { return r != 0 && r&^knownBufferReasons == 0 }
func (r BufferReason) Has(value BufferReason) bool {
	return r&value == value
}

type Limit struct {
	Items int
	Bytes int64
	Span  time.Duration
}

func (l Limit) Valid() bool { return l.Items > 0 && l.Bytes >= 0 && l.Span >= 0 }

// Island is one maximal synchronous execution region. Source and sink I/O
// appear as single-node islands; adjacent Processor nodes share one island.
type Island struct {
	ID    string
	Nodes []string
}

// Buffer projects the queue policy selected for one logical edge. A logical
// edge can expand into several private runtime queues.
type Buffer struct {
	ID       string
	FromNode string
	FromPort string
	ToNode   string
	ToPort   string
	Limit    Limit
	Reason   BufferReason
}

// FanIn records the policy and timestamp tolerance selected for one
// many-input port. Its ordered inputs remain the node's port descriptors and
// the logical graph edges.
//
// Direct reports that the port declared flow.Direct and planning confirmed it:
// one routed producer drives the port from inside the same synchronous island,
// so the order it emits in is the order the port observes.
type FanIn struct {
	Node      string
	Port      string
	Policy    flow.FanInPolicy
	Tolerance time.Duration
	Direct    bool
}

// Runtime is the inert projection of private Program specialization.
type Runtime struct {
	Executable bool
	Islands    []Island
	Buffers    []Buffer
	FanIns     []FanIn
}

func cloneRuntime(value Runtime) Runtime {
	value.Islands = append([]Island(nil), value.Islands...)
	for index := range value.Islands {
		value.Islands[index].Nodes = append([]string(nil), value.Islands[index].Nodes...)
	}
	value.Buffers = append([]Buffer(nil), value.Buffers...)
	value.FanIns = append([]FanIn(nil), value.FanIns...)
	return value
}

func normalizeRuntime(value Runtime) Runtime {
	value = cloneRuntime(value)
	sort.Slice(value.Islands, func(left, right int) bool { return value.Islands[left].ID < value.Islands[right].ID })
	sort.Slice(value.Buffers, func(left, right int) bool { return value.Buffers[left].ID < value.Buffers[right].ID })
	sort.Slice(value.FanIns, func(left, right int) bool {
		leftKey := value.FanIns[left].Node + ":" + value.FanIns[left].Port
		rightKey := value.FanIns[right].Node + ":" + value.FanIns[right].Port
		return leftKey < rightKey
	})
	return value
}
