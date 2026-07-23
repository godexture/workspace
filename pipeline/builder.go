package pipeline

import (
	"errors"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

func Link[T any, A node.OutputNode[T], B node.InputNode[T]](nodeA A, portA string, nodeB B, portB string) error {
	return LinkWithBufferSize(nodeA, portA, nodeB, portB, 100)
}

// LinkWithBufferSize connects two nodes using a channel edge with the requested
// capacity. A small capacity is useful for pipelines that must apply strict
// backpressure to large packets or frames.
func LinkWithBufferSize[T any, A node.OutputNode[T], B node.InputNode[T]](nodeA A, portA string, nodeB B, portB string, bufferSize int) error {
	if bufferSize < 0 {
		return fmt.Errorf("invalid edge buffer size: %d", bufferSize)
	}

	outPort, okA := nodeA.OutputPorts()[portA]
	if !okA {
		return fmt.Errorf("output port '%s' not found on node A", portA)
	}

	inPort, okB := nodeB.InputPorts()[portB]
	if !okB {
		return fmt.Errorf("input port '%s' not found on node B", portB)
	}

	return connectPorts(outPort, inPort, NewChanEdge[T](bufferSize))
}

func connectPorts[T any](outPort *node.OutPort[T], inPort *node.InPort[T], edge node.Edge[T]) error {
	outPort.Connect(edge)
	inPort.Connect(edge)
	return nil
}

func LinkAny(nodeA node.Node, portA string, nodeB node.Node, portB string) error {
	if outA, okA := nodeA.(node.OutputNode[*media.Packet]); okA {
		if inB, okB := nodeB.(node.InputNode[*media.Packet]); okB {
			return Link(outA, portA, inB, portB)
		}
	}
	if outA, okA := nodeA.(node.OutputNode[media.Frame]); okA {
		if inB, okB := nodeB.(node.InputNode[media.Frame]); okB {
			return Link(outA, portA, inB, portB)
		}
	}
	return fmt.Errorf("incompatible nodes or port types: %T and %T", nodeA, nodeB)
}

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Build(geo *Geometry, options ...BuildOption) (*Pipeline, error) {
	if geo == nil {
		return nil, fmt.Errorf("%w: geometry is nil", ErrInvalidPipeline)
	}
	config := buildConfig{}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	if config.observation > ObservationMetrics {
		return nil, fmt.Errorf("%w: unknown observation mode %d", ErrInvalidPipeline, config.observation)
	}
	nodeDefs, edges, resourceClosers, err := geo.take()
	if err != nil {
		return nil, err
	}

	nodeMap := make(map[string]node.Node)
	nodeList := make([]node.Node, 0, len(nodeDefs))

	for _, n := range nodeDefs {
		nodeMap[n.ID] = n.Node
		nodeList = append(nodeList, n.Node)
	}

	var metricsByEdge []*edgeMetrics
	if config.observation != ObservationOff {
		metricsByEdge = make([]*edgeMetrics, len(edges))
	}
	for i, e := range edges {
		fromNode, ok := nodeMap[e.FromNode]
		if !ok {
			return nil, errors.Join(
				fmt.Errorf("%w: node not found: %s", ErrInvalidPipeline, e.FromNode),
				closeNodes(nodeList),
				closeResources(resourceClosers),
			)
		}
		toNode, ok := nodeMap[e.ToNode]
		if !ok {
			return nil, errors.Join(
				fmt.Errorf("%w: node not found: %s", ErrInvalidPipeline, e.ToNode),
				closeNodes(nodeList),
				closeResources(resourceClosers),
			)
		}

		observe := config.observation == ObservationMetrics || config.observation == ObservationProgress && e.ProgressSource
		var metrics *edgeMetrics
		if observe {
			metrics = &edgeMetrics{description: e}
			metricsByEdge[i] = metrics
		}
		progressOnly := config.observation == ObservationProgress && e.ProgressSource
		if err := linkAnyConfigured(fromNode, e.FromPort, toNode, e.ToPort, metrics, progressOnly); err != nil {
			return nil, errors.Join(
				fmt.Errorf("%w: link %s:%s to %s:%s: %w", ErrInvalidPipeline, e.FromNode, e.FromPort, e.ToNode, e.ToPort, err),
				closeNodes(nodeList),
				closeResources(resourceClosers),
			)
		}
	}

	description := descriptionFromDefinitions(nodeDefs, edges)
	pipeline, err := newPipeline(nodeList, description, config.observation, metricsByEdge, resourceClosers)
	if err != nil {
		return nil, errors.Join(err, closeNodes(nodeList), closeResources(resourceClosers))
	}
	return pipeline, nil
}

func linkAnyConfigured(nodeA node.Node, portA string, nodeB node.Node, portB string, metrics *edgeMetrics, progressOnly bool) error {
	if metrics == nil {
		return LinkAny(nodeA, portA, nodeB, portB)
	}
	if outA, okA := nodeA.(node.OutputNode[*media.Packet]); okA {
		if inB, okB := nodeB.(node.InputNode[*media.Packet]); okB {
			if progressOnly {
				return linkProgress(outA, portA, inB, portB, metrics)
			}
			return linkObserved(outA, portA, inB, portB, metrics)
		}
	}
	if outA, okA := nodeA.(node.OutputNode[media.Frame]); okA {
		if inB, okB := nodeB.(node.InputNode[media.Frame]); okB {
			if progressOnly {
				return linkProgress(outA, portA, inB, portB, metrics)
			}
			return linkObserved(outA, portA, inB, portB, metrics)
		}
	}
	return fmt.Errorf("incompatible nodes or port types: %T and %T", nodeA, nodeB)
}

func linkProgress[T any, A node.OutputNode[T], B node.InputNode[T]](nodeA A, portA string, nodeB B, portB string, metrics *edgeMetrics) error {
	outPort, ok := nodeA.OutputPorts()[portA]
	if !ok {
		return fmt.Errorf("output port '%s' not found on node A", portA)
	}
	inPort, ok := nodeB.InputPorts()[portB]
	if !ok {
		return fmt.Errorf("input port '%s' not found on node B", portB)
	}
	edge := &progressEdge[T]{ChanEdge: NewChanEdge[T](100), metrics: metrics}
	return connectPorts(outPort, inPort, edge)
}

func linkObserved[T any, A node.OutputNode[T], B node.InputNode[T]](nodeA A, portA string, nodeB B, portB string, metrics *edgeMetrics) error {
	outPort, ok := nodeA.OutputPorts()[portA]
	if !ok {
		return fmt.Errorf("output port '%s' not found on node A", portA)
	}
	inPort, ok := nodeB.InputPorts()[portB]
	if !ok {
		return fmt.Errorf("input port '%s' not found on node B", portB)
	}
	edge := &observedEdge[T]{ChanEdge: NewChanEdge[T](100), metrics: metrics}
	return connectPorts(outPort, inPort, edge)
}
