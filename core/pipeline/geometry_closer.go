package pipeline

import (
	"errors"
	"fmt"

	"github.com/godexture/core/node"
)

// AddResourceCloser registers a shared resource (such as a worker pool held
// by several nodes) to be closed once every node in the eventual Pipeline has
// closed, or immediately if the geometry is abandoned instead of built.
func (g *Geometry) AddResourceCloser(closer func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != geometryOpen {
		return fmt.Errorf("%w: geometry does not accept resource closers", ErrInvalidPipeline)
	}
	if closer == nil {
		return fmt.Errorf("%w: resource closer must not be nil", ErrInvalidPipeline)
	}
	g.resourceClosers = append(g.resourceClosers, closer)
	return nil
}

func (g *Geometry) Close() error {
	g.mu.Lock()
	if g.state == geometryTransferred {
		g.mu.Unlock()
		return nil
	}
	if g.state == geometryClosing {
		done := g.done
		g.mu.Unlock()
		<-done
		g.mu.Lock()
		err := g.closeErr
		g.mu.Unlock()
		return err
	}
	if g.state == geometryClosed {
		err := g.closeErr
		g.mu.Unlock()
		return err
	}
	g.state = geometryClosing
	nodes := g.nodes
	closers := g.resourceClosers
	g.nodes = nil
	g.edges = nil
	g.resourceClosers = nil
	g.mu.Unlock()

	owned := make([]node.Node, len(nodes))
	for i := range nodes {
		owned[i] = nodes[i].Node
	}
	err := closeNodes(owned)
	err = errors.Join(err, closeResources(closers))

	g.mu.Lock()
	g.closeErr = err
	g.state = geometryClosed
	close(g.done)
	g.mu.Unlock()
	return err
}

func closeResources(closers []func() error) error {
	var result error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i](); err != nil {
			result = errors.Join(result, fmt.Errorf("close resource %d: %w", i, err))
		}
	}
	return result
}
