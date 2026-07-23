package pipeline

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"golang.org/x/sync/errgroup"
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

func (p *Pipeline) Run(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: run context is nil", ErrInvalidPipeline)
	}
	if err := p.Prepare(ctx); err != nil {
		return err
	}

	p.mu.Lock()
	if p.state != pipelinePrepared {
		state := p.state
		p.mu.Unlock()
		return fmt.Errorf("%w: cannot run pipeline in state %s", ErrInvalidPipeline, state)
	}
	runContext, cancel := context.WithCancel(ctx)
	p.state = pipelineRunning
	p.cancel = cancel
	p.startedAt = time.Now()
	p.mu.Unlock()

	var runErr error
	if p.observation == ObservationMetrics {
		runErr = runNodesObservedSelected(runContext, p.runNodes, p.runIndexes, p.nodeMetrics)
	} else {
		runErr = runNodes(runContext, p.runNodes)
	}
	cancel()
	return p.finish(runErr)
}

// Prepare performs resource-dependent node setup. It is safe to call more
// than once; Run calls it automatically when needed.
func (p *Pipeline) Prepare(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: prepare context is nil", ErrInvalidPipeline)
	}

	p.mu.Lock()
	switch p.state {
	case pipelinePrepared, pipelineRunning:
		p.mu.Unlock()
		return nil
	case pipelineReady:
		p.state = pipelinePreparing
		p.prepareDone = make(chan struct{})
	case pipelinePreparing:
		done := p.prepareDone
		p.mu.Unlock()
		<-done
		p.mu.Lock()
		err := p.prepareErr
		p.mu.Unlock()
		return err
	default:
		state := p.state
		p.mu.Unlock()
		return fmt.Errorf("%w: cannot prepare pipeline in state %s", ErrInvalidPipeline, state)
	}
	nodes := append([]node.Node(nil), p.nodes...)
	prepareNodes := append([]node.Node(nil), p.prepareNodes...)
	preloadNodes := append([]node.StagedInput(nil), p.preloadNodes...)
	description := p.description.Clone()
	p.mu.Unlock()

	for i, current := range nodes {
		if err := ctx.Err(); err != nil {
			return p.finishPrepare(err)
		}
		preparer, ok := current.(registry.Preparer)
		if !ok {
			continue
		}
		var grant registry.ResourceGrant
		if i < len(description.Nodes) {
			grant = description.Nodes[i].Resources
		}
		if err := preparer.Prepare(grant); err != nil {
			return p.finishPrepare(fmt.Errorf("prepare node %d (%T): %w", i, current, err))
		}
	}
	if err := runPreparation(ctx, prepareNodes, preloadNodes); err != nil {
		return p.finishPrepare(err)
	}

	p.mu.Lock()
	if p.state == pipelinePreparing {
		p.state = pipelinePrepared
		p.prepareErr = nil
		close(p.prepareDone)
		p.prepareDone = nil
	}
	p.mu.Unlock()
	return nil
}

func (p *Pipeline) finishPrepare(prepareErr error) error {
	p.mu.Lock()
	p.state = pipelineClosing
	p.finishedAt = time.Now()
	p.mu.Unlock()
	p.completeClose()
	p.mu.Lock()
	result := errors.Join(prepareErr, p.closeErr)
	p.prepareErr = result
	if p.prepareDone != nil {
		close(p.prepareDone)
		p.prepareDone = nil
	}
	p.mu.Unlock()
	return result
}

func (p *Pipeline) Close() error {
	p.mu.Lock()
	switch p.state {
	case pipelineReady, pipelinePrepared:
		p.state = pipelineClosing
		p.mu.Unlock()
		p.completeClose()
	case pipelinePreparing:
		done := p.prepareDone
		p.mu.Unlock()
		<-done
		return p.Close()
	case pipelineRunning:
		cancel := p.cancel
		done := p.done
		p.mu.Unlock()
		cancel()
		<-done
	case pipelineClosing:
		done := p.done
		p.mu.Unlock()
		<-done
	case pipelineClosed:
		p.mu.Unlock()
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeErr
}

func (p *Pipeline) finish(runErr error) error {
	p.mu.Lock()
	p.state = pipelineClosing
	p.finishedAt = time.Now()
	p.mu.Unlock()
	p.completeClose()

	p.mu.Lock()
	defer p.mu.Unlock()
	return errors.Join(runErr, p.closeErr)
}

func (p *Pipeline) Description() Description {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.description.Clone()
}

func (p *Pipeline) Snapshot() Snapshot {
	now := time.Now()
	p.mu.Lock()
	state := p.state.String()
	started := p.startedAt
	finished := p.finishedAt
	edges := append([]*edgeMetrics(nil), p.edgeMetrics...)
	nodes := append([]*nodeMetrics(nil), p.nodeMetrics...)
	description := p.description.Clone()
	p.mu.Unlock()

	end := finished
	if end.IsZero() && !started.IsZero() {
		end = now
	}
	var elapsed time.Duration
	if !started.IsZero() {
		elapsed = end.Sub(started)
	}
	snapshot := Snapshot{
		State:      state,
		StartedAt:  started,
		FinishedAt: finished,
		Elapsed:    elapsed,
		Nodes:      make([]NodeSnapshot, len(description.Nodes)),
		Edges:      make([]EdgeSnapshot, len(description.Edges)),
	}
	for i, current := range description.Nodes {
		snapshot.Nodes[i] = NodeSnapshot{Description: current, State: "unobserved"}
		if i < len(nodes) && nodes[i] != nil {
			snapshot.Nodes[i] = nodes[i].snapshot(now)
		}
	}
	for i, current := range description.Edges {
		snapshot.Edges[i] = EdgeSnapshot{Description: current}
		if i < len(edges) && edges[i] != nil {
			snapshot.Edges[i] = edges[i].snapshot()
		}
	}
	return snapshot
}

func (p *Pipeline) completeClose() {
	// Resources (e.g. a worker pool shared by several nodes) are closed only
	// after every node has closed, since a node's own Close may still submit
	// or await work on a shared resource.
	closeErr := errors.Join(closeNodes(p.nodes), closeResources(p.resourceClosers))

	p.mu.Lock()
	p.closeErr = closeErr
	p.state = pipelineClosed
	p.nodes = nil
	p.prepareNodes = nil
	p.preloadNodes = nil
	p.runNodes = nil
	p.resourceClosers = nil
	close(p.done)
	p.mu.Unlock()
}

func runPreparation(ctx context.Context, nodes []node.Node, preloaders []node.StagedInput) error {
	group, groupContext := errgroup.WithContext(ctx)
	for _, current := range nodes {
		current := current
		group.Go(func() error { return current.Start(groupContext) })
	}
	for _, current := range preloaders {
		current := current
		group.Go(func() error { return current.Preload(groupContext) })
	}
	return group.Wait()
}

func runNodes(ctx context.Context, nodes []node.Node) error {
	group, groupContext := errgroup.WithContext(ctx)
	for _, current := range nodes {
		current := current
		group.Go(func() error {
			return current.Start(groupContext)
		})
	}
	return group.Wait()
}

func runNodesObserved(ctx context.Context, nodes []node.Node, metrics []*nodeMetrics) error {
	group, groupContext := errgroup.WithContext(ctx)
	for i, current := range nodes {
		current := current
		metric := metrics[i]
		group.Go(func() error {
			metric.start()
			err := current.Start(groupContext)
			metric.finish(err)
			return err
		})
	}
	return group.Wait()
}

func runNodesObservedSelected(ctx context.Context, nodes []node.Node, indexes []int, metrics []*nodeMetrics) error {
	group, groupContext := errgroup.WithContext(ctx)
	for i, current := range nodes {
		current := current
		index := indexes[i]
		group.Go(func() error {
			metric := metrics[index]
			metric.start()
			err := current.Start(groupContext)
			metric.finish(err)
			return err
		})
	}
	return group.Wait()
}

func closeNodes(nodes []node.Node) error {
	var result error
	for i := len(nodes) - 1; i >= 0; i-- {
		if err := nodes[i].Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close node %d (%T): %w", i, nodes[i], err))
		}
	}
	return result
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
