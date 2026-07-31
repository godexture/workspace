package nodes

import (
	"context"
	"fmt"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/node"
)

type frameTeeNode struct {
	in     *node.InPort[media.Frame]
	first  *node.OutPort[media.Frame]
	second *node.OutPort[media.Frame]
}

func NewFrameTee() *frameTeeNode {
	return &frameTeeNode{
		in:     node.NewInPort[media.Frame]("in"),
		first:  node.NewOutPort[media.Frame]("first", media.StreamInfo{}),
		second: node.NewOutPort[media.Frame]("second", media.StreamInfo{}),
	}
}

func (n *frameTeeNode) Start(ctx context.Context) error {
	first := n.first.Edge()
	second := n.second.Edge()
	if n.in.Edge() == nil || first == nil || second == nil {
		return fmt.Errorf("frame tee ports not connected")
	}
	defer first.Close()
	defer second.Close()

	return consumeUntilEOF(ctx, n.in, func(frame media.Frame) error {
		if err := retainAndPushFrame(ctx, first, frame); err != nil {
			return err
		}
		if err := retainAndPushFrame(ctx, second, frame); err != nil {
			return err
		}
		return nil
	})
}

func (n *frameTeeNode) Close() error { return nil }

func (n *frameTeeNode) InputPorts() map[string]*node.InPort[media.Frame] {
	return map[string]*node.InPort[media.Frame]{"in": n.in}
}

func (n *frameTeeNode) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{"first": n.first, "second": n.second}
}
