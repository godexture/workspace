// Package run specializes a compiled graph into dense execution islands and
// typed edge factories. The public Plan receives only an inert projection.
package run

import (
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/run/drive"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/stream"
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
	Inputs    flow.Descriptors[stream.Descriptor]
	Outputs   flow.Descriptors[stream.Descriptor]
	Execution any
}

type node struct {
	id             job.NodeID
	shape          flow.Shape
	binding        drive.Binding
	kind           drive.Kind
	inputs         flow.Descriptors[stream.Descriptor]
	outputs        flow.Descriptors[stream.Descriptor]
	tolerance      time.Duration
	toleranceTicks int64
}

type Template struct {
	nodes       []node
	edges       []edge
	connections []connection
	incoming    [][]int
	outgoing    [][]int
	projection  plan.Runtime
	executable  bool
}

func Compile(values []Node, logicalEdges []job.Edge, queuePolicy job.QueuePolicy, alignmentPolicy job.AlignmentPolicy) (Template, error) {
	if len(values) == 0 || !queuePolicy.Valid() || !alignmentPolicy.Valid() {
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
		if token.Kind() == drive.Joiner && token.FanIn() != flow.ZipFanIn && token.FanIn() != flow.SerialFanIn {
			return Template{}, ErrUnsupportedFan
		}
		result.nodes[index] = node{
			id:      value.ID,
			shape:   value.Shape.Clone(),
			binding: token,
			kind:    token.Kind(),
			inputs:  copyDescriptors(value.Inputs),
			outputs: copyDescriptors(value.Outputs),
		}
	}
	result.edges = make([]edge, len(logicalEdges))
	for index, value := range logicalEdges {
		from, fromOK := byID[value.From().Node()]
		to, toOK := byID[value.To().Node()]
		if !value.Valid() || !fromOK || !toOK {
			return Template{}, ErrTopology
		}
		if from >= to {
			return Template{}, ErrTopologyOrder
		}
		result.edges[index] = edge{value: value, from: from, to: to}
	}
	if missing {
		return result, nil
	}
	if err := result.expandConnections(queuePolicy); err != nil {
		return Template{}, err
	}
	for index := range result.nodes {
		if result.nodes[index].kind != drive.Joiner {
			continue
		}
		tolerance, ticks, err := result.alignmentTolerance(index, alignmentPolicy)
		if err != nil {
			return Template{}, err
		}
		result.nodes[index].tolerance = tolerance
		result.nodes[index].toleranceTicks = ticks
	}
	if err := result.validateEdges(); err != nil {
		return Template{}, err
	}
	if err := result.validateFanOutSafety(); err != nil {
		return Template{}, err
	}
	if err := result.validateFanInLimits(); err != nil {
		return Template{}, err
	}
	result.placeBuffers()
	if err := result.validateDirectInputs(); err != nil {
		return Template{}, err
	}
	if err := result.validatePriorInputs(); err != nil {
		return Template{}, err
	}
	result.projection = result.project()
	result.executable = true
	return result, nil
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
		case drive.Source, drive.RoutedSource:
			if len(t.incoming[index]) != 0 || len(t.outgoing[index]) == 0 {
				return ErrTopology
			}
		case drive.Processor:
			if len(t.incoming[index]) != 1 || len(t.outgoing[index]) == 0 {
				return ErrTopology
			}
		case drive.Router:
			if len(t.incoming[index]) != 1 || len(t.outgoing[index]) == 0 {
				return ErrTopology
			}
		case drive.Joiner:
			minimumInputs := 2
			if value.binding.FanIn() == flow.SerialFanIn {
				minimumInputs = 1
			}
			if len(t.incoming[index]) < minimumInputs || len(t.outgoing[index]) == 0 {
				return ErrTopology
			}
		case drive.Sink:
			if len(t.incoming[index]) != 1 || len(t.outgoing[index]) != 0 {
				return ErrTopology
			}
		default:
			return ErrTopology
		}
		accepted := value.binding.Inputs()
		if len(accepted) == 0 {
			accepted = []string{value.binding.Input()}
		}
		for _, connectionIndex := range t.incoming[index] {
			if !slices.Contains(accepted, t.edges[t.connections[connectionIndex].logical].value.To().ID()) {
				return ErrTopology
			}
		}
		for _, connectionIndex := range t.outgoing[index] {
			if t.edges[t.connections[connectionIndex].logical].value.From().ID() != value.binding.Output() {
				return ErrTopology
			}
			if value.kind != drive.Router && value.kind != drive.RoutedSource && t.connections[connectionIndex].route != 0 {
				return ErrTopology
			}
		}
	}
	return nil
}
