package engine

import (
	"context"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type Decoder struct {
	engine DecoderEngine

	inEdge  node.Edge[*media.Packet]
	outEdge node.Edge[*media.Frame]
}

func WrapDecoder(engine DecoderEngine) node.Node {
	return &Decoder{
		engine: engine,
	}
}

func (n *Decoder) Start(ctx context.Context) error {
	defer n.outEdge.Close()

	for {
		pkt, err := n.inEdge.Pull(ctx)
		if err == io.EOF {
			return n.engine.Flush()
		} else if err != nil {
			return err
		}

		if err := n.engine.SendPacket(pkt); err != nil {
			return err
		}

		for {
			frame, err := n.engine.ReceiveFrame()
			if err == ErrEAGAIN {
				break
			} else if err != nil {
				return err
			}

			if err := n.outEdge.Push(ctx, frame); err != nil {
				return err
			}
		}
	}
}

func (n *Decoder) InputPorts() map[string]node.InPort[*media.Packet] {
	return map[string]node.InPort[*media.Packet]{
		"in": node.NewInPort[*media.Packet]("in", nil),
	}
}

func (n *Decoder) OutputPorts() map[string]node.OutPort[*media.Frame] {
	return map[string]node.OutPort[*media.Frame]{
		"out": node.NewOutPort[*media.Frame]("out", media.StreamInfo{}),
	}
}
