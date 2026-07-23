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
	preparation, err := planPreparation(nodeDefs, edges, nodeMap)
	if err != nil {
		return nil, errors.Join(err, closeNodes(nodeList), closeResources(resourceClosers))
	}
	pipeline, err := newPipeline(nodeList, description, config.observation, metricsByEdge, resourceClosers, preparation)
	if err != nil {
		return nil, errors.Join(err, closeNodes(nodeList), closeResources(resourceClosers))
	}
	return pipeline, nil
}

type preparationPlan struct {
	nodes    []node.Node
	preloads []node.StagedInput
	run      []node.Node
	runIndex []int
}

func planPreparation(definitions []NodeDef, edges []EdgeDef, nodes map[string]node.Node) (preparationPlan, error) {
	incoming := make(map[string][]EdgeDef)
	for _, edge := range edges {
		incoming[edge.ToNode] = append(incoming[edge.ToNode], edge)
	}
	preloadRoots := make(map[string]struct{})
	preloads := make([]node.StagedInput, 0)
	for _, definition := range definitions {
		staged, ok := definition.Node.(node.StagedInput)
		if !ok {
			continue
		}
		hasPreload := false
		for port, phase := range staged.InputPhases() {
			if phase != node.InputPhasePreload {
				continue
			}
			hasPreload = true
			connected := false
			for _, edge := range incoming[definition.ID] {
				if edge.ToPort == port {
					connected = true
					break
				}
			}
			if !connected {
				return preparationPlan{}, fmt.Errorf("%w: preload port %s:%s is not connected", ErrInvalidPipeline, definition.ID, port)
			}
		}
		if hasPreload {
			preloadRoots[definition.ID] = struct{}{}
			preloads = append(preloads, staged)
		}
	}

	prepareIDs := make(map[string]struct{})
	var visit func(string) error
	visit = func(id string) error {
		if _, root := preloadRoots[id]; root {
			return fmt.Errorf("%w: preload path cannot pass through consumer %s", ErrInvalidPipeline, id)
		}
		if _, exists := prepareIDs[id]; exists {
			return nil
		}
		if _, exists := nodes[id]; !exists {
			return fmt.Errorf("%w: preload node not found: %s", ErrInvalidPipeline, id)
		}
		prepareIDs[id] = struct{}{}
		for _, edge := range incoming[id] {
			if err := visit(edge.FromNode); err != nil {
				return err
			}
		}
		return nil
	}
	for _, edge := range edges {
		if _, root := preloadRoots[edge.ToNode]; root {
			staged := nodes[edge.ToNode].(node.StagedInput)
			if staged.InputPhases()[edge.ToPort] == node.InputPhasePreload {
				if err := visit(edge.FromNode); err != nil {
					return preparationPlan{}, err
				}
			}
		}
	}

	plan := preparationPlan{preloads: preloads}
	for i, definition := range definitions {
		current := definition.Node
		if _, prepare := prepareIDs[definition.ID]; prepare {
			plan.nodes = append(plan.nodes, current)
			continue
		}
		plan.run = append(plan.run, current)
		plan.runIndex = append(plan.runIndex, i)
	}
	return plan, nil
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
