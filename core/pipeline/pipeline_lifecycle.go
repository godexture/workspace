package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/godexture/core/node"
	"github.com/godexture/core/registry"
	"golang.org/x/sync/errgroup"
)

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
