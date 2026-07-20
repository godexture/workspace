package nodes

import (
	"context"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type frameDiscardNode struct {
	in *node.InPort[media.Frame]
}

func NewFrameDiscard() *frameDiscardNode {
	return &frameDiscardNode{in: node.NewInPort[media.Frame]("in", nil)}
}

func (n *frameDiscardNode) Start(ctx context.Context) error {
	return consumeUntilEOF(ctx, n.in, func(media.Frame) error { return nil })
}

func (n *frameDiscardNode) Close() error { return nil }

func (n *frameDiscardNode) InputPorts() map[string]*node.InPort[media.Frame] {
	return map[string]*node.InPort[media.Frame]{"in": n.in}
}
