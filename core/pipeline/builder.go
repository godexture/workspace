package pipeline

import (
	"errors"
	"fmt"

	"github.com/godexture/godec/core/node"
)

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
