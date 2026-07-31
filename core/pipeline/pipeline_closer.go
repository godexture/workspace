package pipeline

import (
	"errors"
	"fmt"
	"time"

	"github.com/godexture/godec/core/node"
)

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

func closeNodes(nodes []node.Node) error {
	var result error
	for i := len(nodes) - 1; i >= 0; i-- {
		if err := nodes[i].Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close node %d (%T): %w", i, nodes[i], err))
		}
	}
	return result
}
