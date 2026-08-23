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
	if t.nodes[index].binding.FanIn() != flow.ZipFanIn {
		return 0, 0, nil
	}
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

// ErrDirectIsland reports a port that declared flow.Direct in a topology which
// cannot deliver one producer's emit order.
var ErrDirectIsland = errors.New("direct many-input port requires one routed producer in the same synchronous island")

// validateDirectInputs enforces flow.Direct. Serial fan-in already serializes
// callbacks and placeBuffers already keeps its inputs unqueued; what a direct
// port additionally needs is that the calls come from one producer, so their
// order is that producer's own sequence rather than a race between tasks.
func (t Template) validateDirectInputs() error {
	for index, value := range t.nodes {
		for _, port := range value.shape.Inputs {
			if !port.Direct() {
				continue
			}
			if err := t.validateDirectPort(index, port.ID()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t Template) validateDirectPort(index int, port string) error {
	incoming := t.incoming[index]
	if len(incoming) == 0 {
		return errors.Join(ErrTopology, ErrDirectIsland)
	}
	producer := -1
	for _, connectionIndex := range incoming {
		connection := t.connections[connectionIndex]
		if t.edges[connection.logical].value.To().ID() != port {
			continue
		}
		if connection.reason != 0 {
			return errors.Join(ErrTopology, ErrDirectIsland)
		}
		if producer >= 0 && producer != connection.from {
			return errors.Join(ErrTopology, ErrDirectIsland)
		}
		producer = connection.from
	}
	if producer < 0 {
		return errors.Join(ErrTopology, ErrDirectIsland)
	}
	if kind := t.nodes[producer].kind; kind != drive.Router && kind != drive.RoutedSource {
		return errors.Join(ErrTopology, ErrDirectIsland)
	}
	return nil
}

func (t Template) validateFanOutSafety() error {
	for index := range t.edges {
		if !t.logicalFanOut(index) {
			continue
		}
		from := t.nodes[t.edges[index].from]
		if !from.binding.FanoutSafe() {
			return errors.Join(ErrTopology, drive.ErrForkTrait)
		}
	}
	return nil
}

func (t Template) validateFanInLimits() error {
	for index, value := range t.nodes {
		if value.kind != drive.Joiner || value.binding.FanIn() != flow.ZipFanIn {
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
		// A serial fan-in exists to observe the order its producer emits in.
		// A queue on that connection replaces the producer with one drain task
		// per route, so the ordinals arrive interleaved and the only thing the
		// policy promised is gone. No reason to buffer outweighs that.
		if to.kind == drive.Joiner && to.binding.FanIn() == flow.SerialFanIn {
			continue
		}
		if from.kind == drive.Source || from.kind == drive.RoutedSource {
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

// ErrPriorBranch reports a prior input whose branch is not independent of the
// node's other inputs.
var ErrPriorBranch = errors.New("prior input shares an upstream node with the inputs it precedes")

// validatePriorInputs enforces flow.Prior. Reading one input to completion
// before the others works because the others simply wait: their queues fill
// and their producers block. That only bounds anything while the two branches
// are independent -- a node feeding both would have to buffer everything it
// produced for the waiting side in order to finish the prior one, which is the
// unbounded hold the declaration exists to avoid.
func (t Template) validatePriorInputs() error {
	for index, value := range t.nodes {
		prior, others := map[int]struct{}{}, map[int]struct{}{}
		found := false
		for _, port := range value.shape.Inputs {
			found = found || port.Prior()
		}
		if !found {
			continue
		}
		for _, connectionIndex := range t.incoming[index] {
			connection := t.connections[connectionIndex]
			target := t.edges[connection.logical].value.To().ID()
			reached := prior
			if !t.priorPort(value.shape, target) {
				reached = others
			}
			t.reachable(connection.from, reached)
		}
		for node := range prior {
			if _, shared := others[node]; shared {
				return errors.Join(ErrTopology, ErrPriorBranch)
			}
		}
	}
	return nil
}

func (Template) priorPort(shape flow.Shape, id string) bool {
	for _, port := range shape.Inputs {
		if port.ID() == id {
			return port.Prior()
		}
	}
	return false
}

// reachable collects every node that can reach the given one, which is the
// whole of the branch feeding it.
func (t Template) reachable(index int, into map[int]struct{}) {
	if _, seen := into[index]; seen {
		return
	}
	into[index] = struct{}{}
	for _, connectionIndex := range t.incoming[index] {
		t.reachable(t.connections[connectionIndex].from, into)
	}
}
