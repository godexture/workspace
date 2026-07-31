package pipeline

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/godexture/core/node"
)

type pipelineState uint8

const (
	pipelineReady pipelineState = iota
	pipelinePreparing
	pipelinePrepared
	pipelineRunning
	pipelineClosing
	pipelineClosed
)

// Pipeline owns its nodes for their complete lifecycle. A Pipeline is
// single-use: Run may be called exactly once, and always closes every node
// before returning.
type Pipeline struct {
	mu              sync.Mutex
	state           pipelineState
	nodes           []node.Node
	prepareNodes    []node.Node
	preloadNodes    []node.StagedInput
	runNodes        []node.Node
	runIndexes      []int
	description     Description
	observation     ObservationMode
	edgeMetrics     []*edgeMetrics
	nodeMetrics     []*nodeMetrics
	resourceClosers []func() error
	startedAt       time.Time
	finishedAt      time.Time
	cancel          context.CancelFunc
	prepareDone     chan struct{}
	prepareErr      error
	done            chan struct{}
	closeErr        error
}

func New(nodes ...node.Node) (*Pipeline, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: pipeline has no nodes", ErrInvalidPipeline)
	}
	owned := append([]node.Node(nil), nodes...)
	valid := make([]node.Node, 0, len(owned))
	nilIndex := -1
	for i, n := range owned {
		if isNilNode(n) {
			if nilIndex < 0 {
				nilIndex = i
			}
			continue
		}
		valid = append(valid, n)
	}
	if nilIndex >= 0 {
		return nil, errors.Join(
			fmt.Errorf("%w: node %d is nil", ErrInvalidPipeline, nilIndex),
			closeNodes(valid),
		)
	}
	description := Description{Nodes: make([]NodeDescription, len(owned))}
	for i := range owned {
		description.Nodes[i].ID = fmt.Sprintf("node:%d", i)
	}
	return newPipeline(owned, description, ObservationOff, nil, nil, preparationPlan{run: owned, runIndex: makeIndexes(len(owned))})
}

func newPipeline(nodes []node.Node, description Description, observation ObservationMode, edges []*edgeMetrics, resourceClosers []func() error, preparation preparationPlan) (*Pipeline, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: pipeline has no nodes", ErrInvalidPipeline)
	}
	pipeline := &Pipeline{
		nodes:           append([]node.Node(nil), nodes...),
		prepareNodes:    append([]node.Node(nil), preparation.nodes...),
		preloadNodes:    append([]node.StagedInput(nil), preparation.preloads...),
		runNodes:        append([]node.Node(nil), preparation.run...),
		runIndexes:      append([]int(nil), preparation.runIndex...),
		description:     description,
		observation:     observation,
		edgeMetrics:     edges,
		resourceClosers: resourceClosers,
		done:            make(chan struct{}),
	}
	if len(pipeline.runNodes) == 0 {
		pipeline.runNodes = append([]node.Node(nil), nodes...)
		pipeline.runIndexes = makeIndexes(len(nodes))
	}
	if observation == ObservationMetrics {
		pipeline.nodeMetrics = make([]*nodeMetrics, len(nodes))
		for i := range nodes {
			var description NodeDescription
			if i < len(pipeline.description.Nodes) {
				description = pipeline.description.Nodes[i]
			}
			pipeline.nodeMetrics[i] = newNodeMetrics(description)
		}
	}
	return pipeline, nil
}

func makeIndexes(length int) []int {
	indexes := make([]int, length)
	for i := range indexes {
		indexes[i] = i
	}
	return indexes
}

func isNilNode(value node.Node) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func (s pipelineState) String() string {
	switch s {
	case pipelineReady:
		return "ready"
	case pipelinePreparing:
		return "preparing"
	case pipelinePrepared:
		return "prepared"
	case pipelineRunning:
		return "running"
	case pipelineClosing:
		return "closing"
	case pipelineClosed:
		return "closed"
	default:
		return "unknown"
	}
}
