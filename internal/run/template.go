// Package run specializes a compiled graph into dense execution islands and
// typed edge factories. The public Plan receives only an inert projection.
package run

import (
	"errors"
	"reflect"
	"sort"
	"strconv"
	"time"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/internal/run/queue"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plan"
)

var (
	ErrTopology       = errors.New("runtime topology does not match typed execution bindings")
	ErrTopologyOrder  = errors.New("runtime nodes are not in topological order")
	ErrUnsupportedFan = errors.New("fan-in policy has no runtime implementation")
)

type Node struct {
	ID        job.NodeID
	Shape     flow.Shape
	Outputs   flow.Descriptors[stream.Descriptor]
	Execution any
}

type node struct {
	id      job.NodeID
	shape   flow.Shape
	binding drive.Binding
	kind    drive.Kind
	base    timing.Base
}

type edge struct {
	value  job.Edge
	from   int
	to     int
	limit  queue.Limit
	reason plan.BufferReason
}

type Template struct {
	nodes      []node
	edges      []edge
	incoming   [][]int
	outgoing   [][]int
	projection plan.Runtime
	executable bool
}

func Compile(values []Node, connections []job.Edge, policy job.QueuePolicy) (Template, error) {
	if len(values) == 0 || !policy.Valid() {
		return Template{}, ErrTopology
	}
	result := Template{
		nodes:    make([]node, len(values)),
		incoming: make([][]int, len(values)),
		outgoing: make([][]int, len(values)),
	}
	byID := make(map[job.NodeID]int, len(values))
	missing := false
	for index, value := range values {
		if !value.ID.Valid() || value.Shape.Empty() {
			return Template{}, ErrTopology
		}
		if _, exists := byID[value.ID]; exists {
			return Template{}, ErrTopology
		}
		byID[value.ID] = index
		token, ok := value.Execution.(drive.Binding)
		if !ok || !token.Valid() {
			missing = true
			result.nodes[index] = node{id: value.ID, shape: value.Shape.Clone()}
			continue
		}
		if err := token.Validate(value.Shape); err != nil {
			return Template{}, errors.Join(ErrTopology, err)
		}
		if token.Kind() == drive.Joiner && token.FanIn() != flow.ZipFanIn {
			return Template{}, ErrUnsupportedFan
		}
		var base timing.Base
		if token.Kind() != drive.Sink {
			if descriptor, ok := value.Outputs.One(token.Output()); ok {
				base = descriptor.TimeBase()
			}
		}
		result.nodes[index] = node{id: value.ID, shape: value.Shape.Clone(), binding: token, kind: token.Kind(), base: base}
	}
	result.edges = make([]edge, len(connections))
	for index, connection := range connections {
		from, fromOK := byID[connection.From().Node()]
		to, toOK := byID[connection.To().Node()]
		if !connection.Valid() || !fromOK || !toOK {
			return Template{}, ErrTopology
		}
		if from >= to {
			return Template{}, ErrTopologyOrder
		}
		limit, err := result.edgeLimit(from, to, policy)
		if err != nil {
			return Template{}, err
		}
		result.edges[index] = edge{value: connection, from: from, to: to, limit: limit}
		result.outgoing[from] = append(result.outgoing[from], index)
		result.incoming[to] = append(result.incoming[to], index)
	}
	if missing {
		return result, nil
	}
	if err := result.validateEdges(); err != nil {
		return Template{}, err
	}
	if err := result.validateFanInLimits(); err != nil {
		return Template{}, err
	}
	result.placeBuffers()
	result.projection = result.project()
	result.executable = true
	return result, nil
}

func (t Template) validateFanInLimits() error {
	for index, value := range t.nodes {
		if value.kind != drive.Joiner {
			continue
		}
		incoming := t.incoming[index]
		limit := t.edges[incoming[0]].limit
		base := t.nodes[t.edges[incoming[0]].from].base
		for _, edgeIndex := range incoming[1:] {
			edge := t.edges[edgeIndex]
			if edge.limit != limit || limit.Time != 0 && t.nodes[edge.from].base != base {
				return errors.Join(ErrTopology, errors.New("fan-in inputs require identical queue limits and time bases"))
			}
		}
	}
	return nil
}

func (t Template) edgeLimit(from, to int, policy job.QueuePolicy) (queue.Limit, error) {
	limit := queue.Limit{Items: policy.Items}
	measure := t.nodes[from].binding.OutputMeasures()
	if t.nodes[to].kind == drive.Joiner {
		measure = t.nodes[to].binding.InputMeasures()
	}
	if measure.Size {
		limit.Bytes = int64(policy.Bytes)
	}
	if !measure.Time || policy.Window == 0 {
		return limit, nil
	}
	base := t.nodes[from].base
	if !base.Valid() {
		return queue.Limit{}, ErrTopology
	}
	ticks, err := timing.MustBase(1, int64(time.Second)).Rescale(int64(policy.Window), base, timing.RoundCeil)
	if err != nil {
		return queue.Limit{}, errors.Join(ErrTopology, err)
	}
	if ticks < 1 {
		ticks = 1
	}
	limit.Time = ticks
	return limit, nil
}

func (t Template) Executable() bool { return t.executable }
func (t Template) Valid() bool {
	return len(t.nodes) != 0 && len(t.incoming) == len(t.nodes) && len(t.outgoing) == len(t.nodes)
}

func (t Template) Projection() plan.Runtime { return cloneProjection(t.projection) }

func (t Template) Matches(runtime plan.Runtime) bool {
	return reflect.DeepEqual(t.Projection(), runtime)
}

func (t Template) validateEdges() error {
	for index, value := range t.nodes {
		switch value.kind {
		case drive.Source:
			if len(t.incoming[index]) != 0 || len(t.outgoing[index]) == 0 {
				return ErrTopology
			}
		case drive.Processor:
			if len(t.incoming[index]) != 1 || len(t.outgoing[index]) == 0 {
				return ErrTopology
			}
		case drive.Joiner:
			if len(t.incoming[index]) < 2 || len(t.outgoing[index]) == 0 {
				return ErrTopology
			}
		case drive.Sink:
			if len(t.incoming[index]) != 1 || len(t.outgoing[index]) != 0 {
				return ErrTopology
			}
		default:
			return ErrTopology
		}
		for _, edgeIndex := range t.incoming[index] {
			if t.edges[edgeIndex].value.To().ID() != value.binding.Input() {
				return ErrTopology
			}
		}
		for _, edgeIndex := range t.outgoing[index] {
			if t.edges[edgeIndex].value.From().ID() != value.binding.Output() {
				return ErrTopology
			}
		}
	}
	return nil
}

func (t *Template) placeBuffers() {
	for index := range t.edges {
		value := &t.edges[index]
		from := t.nodes[value.from]
		to := t.nodes[value.to]
		if from.kind == drive.Source {
			value.reason |= plan.SourceBuffer
		}
		if to.kind == drive.Sink {
			value.reason |= plan.SinkBuffer
		}
		if len(t.outgoing[value.from]) > 1 {
			value.reason |= plan.FanOutBuffer
		}
		if to.kind == drive.Joiner || len(t.incoming[value.to]) > 1 {
			value.reason |= plan.FanInBuffer
		}
		if from.kind == drive.Joiner {
			value.reason |= plan.FanInBuffer
		}
	}
}

func (t Template) project() plan.Runtime {
	result := plan.Runtime{Executable: true}
	assigned := make([]bool, len(t.nodes))
	for index := range t.nodes {
		if assigned[index] {
			continue
		}
		island := plan.Island{ID: "island-" + strconv.Itoa(len(result.Islands))}
		current := index
		for {
			assigned[current] = true
			island.Nodes = append(island.Nodes, t.nodes[current].id.String())
			if t.nodes[current].kind != drive.Processor || len(t.outgoing[current]) != 1 {
				break
			}
			edgeIndex := t.outgoing[current][0]
			edge := t.edges[edgeIndex]
			if edge.reason != 0 || assigned[edge.to] || t.nodes[edge.to].kind != drive.Processor || len(t.incoming[edge.to]) != 1 {
				break
			}
			current = edge.to
		}
		result.Islands = append(result.Islands, island)
	}
	for _, value := range t.edges {
		if value.reason == 0 {
			continue
		}
		connection := value.value
		result.Buffers = append(result.Buffers, plan.Buffer{
			ID:       edgeKey(connection),
			FromNode: connection.From().Node().String(),
			FromPort: connection.From().ID(),
			ToNode:   connection.To().Node().String(),
			ToPort:   connection.To().ID(),
			Limit:    projectLimit(value.limit),
			Reason:   value.reason,
		})
	}
	for index, value := range t.nodes {
		if value.kind != drive.Joiner {
			continue
		}
		incoming := append([]int(nil), t.incoming[index]...)
		sort.Slice(incoming, func(left, right int) bool {
			return t.edges[incoming[left]].value.From().String() < t.edges[incoming[right]].value.From().String()
		})
		limit := t.edges[incoming[0]].limit
		projection := plan.FanIn{
			Node:      value.id.String(),
			Port:      value.binding.Input(),
			Policy:    value.binding.FanIn(),
			Limit:     projectLimit(limit),
			Watermark: limit.Time,
		}
		for _, edgeIndex := range incoming {
			from := t.edges[edgeIndex].value.From()
			projection.Connections = append(projection.Connections, plan.Connection{FromNode: from.Node().String(), FromPort: from.ID()})
		}
		result.FanIns = append(result.FanIns, projection)
	}
	return result
}

func projectLimit(value queue.Limit) plan.Limit {
	return plan.Limit{Items: value.Items, Bytes: value.Bytes, Time: value.Time}
}

func cloneProjection(value plan.Runtime) plan.Runtime {
	result := value
	result.Islands = append([]plan.Island(nil), value.Islands...)
	for index := range result.Islands {
		result.Islands[index].Nodes = append([]string(nil), value.Islands[index].Nodes...)
	}
	result.Buffers = append([]plan.Buffer(nil), value.Buffers...)
	result.FanIns = append([]plan.FanIn(nil), value.FanIns...)
	for index := range result.FanIns {
		result.FanIns[index].Connections = append([]plan.Connection(nil), value.FanIns[index].Connections...)
	}
	return result
}

func edgeKey(value job.Edge) string { return value.From().String() + "->" + value.To().String() }
