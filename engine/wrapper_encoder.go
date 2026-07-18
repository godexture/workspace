package engine

import (
	"context"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type EncoderAdapter struct {
	engine EncoderEngine
	in     *node.InPort[media.Frame]
	out    *node.OutPort[*media.Packet]
}

func WrapEncoder(engine EncoderEngine) node.Encoder {
	return &EncoderAdapter{
		engine: engine,
		in:     node.NewInPort[media.Frame]("in", nil),
		out:    node.NewOutPort[*media.Packet]("out", media.StreamInfo{}),
	}
}

func (n *EncoderAdapter) Start(ctx context.Context) error {
	in := n.in.Edge()
	out := n.out.Edge()
	if in == nil || out == nil {
		return fmt.Errorf("encoder ports not connected")
	}
	if closer, ok := n.engine.(engineCloser); ok {
		defer closer.Close()
	}

	send := func(f media.Frame) error {
		return n.engine.SendFrame(&f)
	}
	if notifier, ok := n.engine.(outputNotifier); ok {
		return runAsyncCodecLoop(ctx, in, out, send, n.engine.ReceivePacket, n.engine.Flush, notifier)
	}
	return runCodecLoop(ctx, in, out, send, n.engine.ReceivePacket, n.engine.Flush)
}

func (n *EncoderAdapter) InputPorts() map[string]*node.InPort[media.Frame] {
	return map[string]*node.InPort[media.Frame]{
		"in": n.in,
	}
}

func (n *EncoderAdapter) OutputPorts() map[string]*node.OutPort[*media.Packet] {
	return map[string]*node.OutPort[*media.Packet]{
		"out": n.out,
	}
}
