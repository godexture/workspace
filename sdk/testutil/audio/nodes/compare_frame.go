package nodes

import (
	"context"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
	"github.com/godexture/sdk/testutil/audio/pcm"
)

type frameCompareNode struct {
	expected            *node.InPort[media.Frame]
	actual              *node.InPort[media.Frame]
	opts                pcm.CompareOptions
	requireActualLength bool
}

func NewFrameCompare(opts pcm.CompareOptions, requireActualLength bool) *frameCompareNode {
	return &frameCompareNode{
		expected:            node.NewInPort[media.Frame]("expected"),
		actual:              node.NewInPort[media.Frame]("actual"),
		opts:                opts,
		requireActualLength: requireActualLength,
	}
}

func (n *frameCompareNode) InputPorts() map[string]*node.InPort[media.Frame] {
	return map[string]*node.InPort[media.Frame]{"expected": n.expected, "actual": n.actual}
}

func (n *frameCompareNode) Start(ctx context.Context) error {
	if n.expected.Edge() == nil || n.actual.Edge() == nil {
		return fmt.Errorf("frame comparator ports not connected")
	}

	expectedStream := sampleStream{port: n.expected}
	actualStream := sampleStream{port: n.actual}

	var stats pcm.PCMStats
	for !expectedStream.eof || !actualStream.eof || len(expectedStream.next()) > 0 || len(actualStream.next()) > 0 {
		if err := expectedStream.fill(ctx); err != nil {
			return err
		}
		if err := actualStream.fill(ctx); err != nil {
			return err
		}

		expected := expectedStream.next()
		actual := actualStream.next()

		if len(expected) > 0 && len(actual) > 0 {
			count := min(len(expected), len(actual))
			stats.Add(actual[:count], expected[:count])
			expectedStream.advance(count)
			actualStream.advance(count)
			continue
		}
		if expectedStream.eof && len(expected) == 0 && len(actual) > 0 {
			actualStream.advance(len(actual))
		}
		if actualStream.eof && len(actual) == 0 && len(expected) > 0 {
			expectedStream.advance(len(expected))
		}
	}

	if n.requireActualLength && actualStream.count < expectedStream.count {
		return fmt.Errorf("length mismatch: got %d, expected at least %d", actualStream.count, expectedStream.count)
	}
	return stats.Result(n.opts)
}

func (n *frameCompareNode) Close() error { return nil }
