package engine

import (
	"context"
	"fmt"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type DecoderAdapter struct {
	engine DecoderEngine
	in     *node.InPort[*media.Packet]
	out    *node.OutPort[media.Frame]
}

func WrapDecoder(engine DecoderEngine) node.Decoder {
	return &DecoderAdapter{
		engine: engine,
		in:     node.NewInPort[*media.Packet]("in", nil),
		out:    node.NewOutPort[media.Frame]("out", media.StreamInfo{}),
	}
}

func (n *DecoderAdapter) Start(ctx context.Context) error {
	in := n.in.Edge()
	out := n.out.Edge()
	if in == nil || out == nil {
		return fmt.Errorf("decoder ports not connected")
	}

	return runCodecLoop(ctx, in, out,
		func(pkt *media.Packet) error {
			if pkt.Kind == media.PacketKindStreamEnd {
				return nil
			}
			if pkt.Kind != media.PacketKindData {
				return fmt.Errorf("unsupported packet kind: %d", pkt.Kind)
			}
			return n.engine.SendPacket(pkt)
		},
		func() (media.Frame, error) {
			f, err := n.engine.ReceiveFrame()
			if err != nil {
				return nil, err
			}
			if f == nil || *f == nil {
				return nil, fmt.Errorf("decoder returned nil frame")
			}
			return *f, nil
		},
		n.engine.Flush,
	)
}

func (n *DecoderAdapter) InputPorts() map[string]*node.InPort[*media.Packet] {
	return map[string]*node.InPort[*media.Packet]{
		"in": n.in,
	}
}

func (n *DecoderAdapter) OutputPorts() map[string]*node.OutPort[media.Frame] {
	return map[string]*node.OutPort[media.Frame]{
		"out": n.out,
	}
}
