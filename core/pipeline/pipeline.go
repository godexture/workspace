package pipeline

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/godexture/core/node"
	"golang.org/x/sync/errgroup"
)

type pipelineState uint8

const (
	pipelineReady pipelineState = iota
	pipelineRunning
	pipelineClosing
	pipelineClosed
)

// Pipeline owns its nodes for their complete lifecycle. A Pipeline is
// single-use: Run may be called exactly once, and always closes every node
// before returning.
type Pipeline struct {
	mu       sync.Mutex
	state    pipelineState
	nodes    []node.Node
	cancel   context.CancelFunc
	done     chan struct{}
	closeErr error
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
	return &Pipeline{
		nodes: owned,
		done:  make(chan struct{}),
	}, nil
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

	p.mu.Lock()
	if p.state != pipelineReady {
		state := p.state
		p.mu.Unlock()
		return fmt.Errorf("%w: cannot run pipeline in state %s", ErrInvalidPipeline, state)
	}
	runContext, cancel := context.WithCancel(ctx)
	p.state = pipelineRunning
	p.cancel = cancel
	p.mu.Unlock()

	runErr := runNodes(runContext, p.nodes)
	cancel()
	return p.finish(runErr)
}

func (p *Pipeline) Close() error {
	p.mu.Lock()
	switch p.state {
	case pipelineReady:
		p.state = pipelineClosing
		p.mu.Unlock()
		p.completeClose()
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
	p.mu.Unlock()
	p.completeClose()

	p.mu.Lock()
	defer p.mu.Unlock()
	return errors.Join(runErr, p.closeErr)
}

func (p *Pipeline) completeClose() {
	closeErr := closeNodes(p.nodes)

	p.mu.Lock()
	p.closeErr = closeErr
	p.state = pipelineClosed
	p.nodes = nil
	close(p.done)
	p.mu.Unlock()
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
