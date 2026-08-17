package run

import (
	"errors"
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

type edge struct {
	value       job.Edge
	from        int
	to          int
	connections []int
	limit       plan.Limit
	reason      plan.BufferReason
}

type connection struct {
	logical    int
	from       int
	to         int
	descriptor stream.Descriptor
	route      int
	input      int
	limit      queue.Limit
	reason     plan.BufferReason
}

func copyDescriptors(value flow.Descriptors[stream.Descriptor]) flow.Descriptors[stream.Descriptor] {
	return flow.NewDescriptors(value.Bindings()...)
}

func (t *Template) expandConnections(policy job.QueuePolicy) error {
	byTarget := make([]map[string][]int, len(t.nodes))
	for index, value := range t.edges {
		ports := byTarget[value.to]
		if ports == nil {
			ports = make(map[string][]int)
			byTarget[value.to] = ports
		}
		port := value.value.To().ID()
		ports[port] = append(ports[port], index)
	}
	for target, ports := range byTarget {
		portIDs := make([]string, 0, len(ports))
		for port := range ports {
			portIDs = append(portIDs, port)
		}
		sort.Strings(portIDs)
		for _, port := range portIDs {
			indexes := ports[port]
			sort.Slice(indexes, func(left, right int) bool {
				return t.edges[indexes[left]].value.From().String() < t.edges[indexes[right]].value.From().String()
			})
			inputs := t.nodes[target].inputs.At(port)
			offset := 0
			for _, edgeIndex := range indexes {
				logical := &t.edges[edgeIndex]
				outputs := t.nodes[logical.from].outputs.At(logical.value.From().ID())
				if len(outputs) == 0 || len(outputs) > len(inputs)-offset {
					return ErrTopology
				}
				logical.limit = t.logicalLimit(logical.from, logical.to, policy)
				for route, descriptor := range outputs {
					input := inputs[offset+route]
					if !descriptor.SchemaDescriptor().Equal(input.SchemaDescriptor()) || !descriptor.SameState(input) {
						return ErrTopology
					}
					limit, err := t.connectionLimit(logical.from, logical.to, descriptor, policy)
					if err != nil {
						return err
					}
					connectionIndex := len(t.connections)
					t.connections = append(t.connections, connection{logical: edgeIndex, from: logical.from, to: logical.to, descriptor: descriptor, route: route, input: offset + route, limit: limit})
					logical.connections = append(logical.connections, connectionIndex)
					t.outgoing[logical.from] = append(t.outgoing[logical.from], connectionIndex)
					t.incoming[logical.to] = append(t.incoming[logical.to], connectionIndex)
				}
				offset += len(outputs)
			}
			if offset != len(inputs) {
				return ErrTopology
			}
		}
	}
	return nil
}

func (t Template) logicalLimit(from, to int, policy job.QueuePolicy) plan.Limit {
	limit := plan.Limit{Items: policy.Items}
	measure := t.nodes[from].binding.OutputMeasures()
	if t.nodes[to].kind == drive.Joiner {
		measure = t.nodes[to].binding.InputMeasures()
	}
	if measure.Size {
		limit.Bytes = int64(policy.Bytes)
	}
	if measure.Time {
		limit.Span = policy.Span
	}
	return limit
}

func (t Template) connectionLimit(from, to int, descriptor stream.Descriptor, policy job.QueuePolicy) (queue.Limit, error) {
	limit := queue.Limit{Items: policy.Items}
	measure := t.nodes[from].binding.OutputMeasures()
	if t.nodes[to].kind == drive.Joiner {
		measure = t.nodes[to].binding.InputMeasures()
	}
	if measure.Size {
		limit.Bytes = int64(policy.Bytes)
	}
	if !measure.Time || policy.Span == 0 {
		return limit, nil
	}
	base := descriptor.TimeBase()
	if !base.Valid() {
		return queue.Limit{}, ErrTopology
	}
	ticks, err := timing.MustBase(1, int64(time.Second)).Rescale(int64(policy.Span), base, timing.RoundCeil)
	if err != nil {
		return queue.Limit{}, errors.Join(ErrTopology, err)
	}
	if ticks < 1 {
		ticks = 1
	}
	limit.Span = ticks
	return limit, nil
}

func (t Template) alignmentTolerance(index int, policy job.AlignmentPolicy) (time.Duration, int64, error) {
	if policy.Zip == 0 || !t.nodes[index].binding.InputMeasures().Time {
		return 0, 0, nil
	}
	incoming := t.incoming[index]
	if len(incoming) == 0 {
		return 0, 0, nil
	}
	base := t.connections[incoming[0]].descriptor.TimeBase()
	if !base.Valid() {
		return 0, 0, ErrTopology
	}
	ticks, err := timing.MustBase(1, int64(time.Second)).Rescale(int64(policy.Zip), base, timing.RoundCeil)
	if err != nil {
		return 0, 0, errors.Join(ErrTopology, err)
	}
	if ticks < 1 {
		ticks = 1
	}
	return policy.Zip, ticks, nil
}

func (t Template) validateFanInLimits() error {
	for index, value := range t.nodes {
		if value.kind != drive.Joiner {
			continue
		}
		incoming := t.incoming[index]
		if len(incoming) == 0 {
			return ErrTopology
		}
		first := t.connections[incoming[0]]
		for _, connectionIndex := range incoming[1:] {
			connection := t.connections[connectionIndex]
			if connection.limit != first.limit {
				return errors.Join(ErrTopology, errors.New("fan-in inputs require identical physical queue limits"))
			}
			if (first.limit.Span != 0 || value.toleranceTicks != 0) && connection.descriptor.TimeBase() != first.descriptor.TimeBase() {
				return errors.Join(ErrTopology, errors.New("fan-in inputs require identical time bases for queue spans or zip tolerance"))
			}
		}
	}
	return nil
}

func (t *Template) placeBuffers() {
	for index := range t.connections {
		connection := &t.connections[index]
		logical := &t.edges[connection.logical]
		from := t.nodes[connection.from]
		to := t.nodes[connection.to]
		if from.kind == drive.Source {
			connection.reason |= plan.SourceBuffer
		}
		if to.kind == drive.Sink {
			connection.reason |= plan.SinkBuffer
		}
		if t.logicalFanOut(connection.logical) {
			connection.reason |= plan.FanOutBuffer
		}
		if to.kind == drive.Joiner || t.logicalFanIn(connection.logical) {
			connection.reason |= plan.FanInBuffer
		}
		if from.kind == drive.Joiner {
			connection.reason |= plan.FanInBuffer
		}
		logical.reason |= connection.reason
	}
}

func (t Template) logicalFanOut(index int) bool {
	value := t.edges[index].value.From()
	count := 0
	for _, edge := range t.edges {
		if edge.value.From() == value {
			count++
		}
	}
	return count > 1
}

func (t Template) logicalFanIn(index int) bool {
	value := t.edges[index].value.To()
	count := 0
	for _, edge := range t.edges {
		if edge.value.To() == value {
			count++
		}
	}
	return count > 1
}

func edgeKey(value job.Edge) string { return value.From().String() + "->" + value.To().String() }

func connectionKey(value job.Edge, route, input int) string {
	return edgeKey(value) + "[" + strconv.Itoa(route) + ":" + strconv.Itoa(input) + "]"
}
