package engine

import (
	"context"
	"fmt"
	"io"

	"github.com/godexture/godec/core/domain/media"
	"github.com/godexture/godec/core/domain/metadata"
	"github.com/godexture/godec/core/node"
)

type MuxerAdapter struct {
	engine    MuxerEngine
	lifecycle engineLifecycle
	in        *node.InPort[*media.Packet]
}

func WrapMuxer(engine MuxerEngine) node.Muxer {
	return &MuxerAdapter{
		engine:    engine,
		lifecycle: newEngineLifecycle(engine),
		in:        node.NewInPort[*media.Packet]("in"),
	}
}

func (n *MuxerAdapter) Close() error {
	return n.lifecycle.Close()
}

func (n *MuxerAdapter) Start(ctx context.Context) error {
	in := n.in.Edge()
	if in == nil {
		return fmt.Errorf("muxer input not connected")
	}

	if err := n.engine.WriteHeader(); err != nil {
		return err
	}

	for {
		pkt, err := in.Pull(ctx)
		if err == io.EOF {
			return n.engine.WriteTrailer()
		} else if err != nil {
			return err
		}

		err = n.engine.WritePacket(0, pkt)
		pkt.Release()
		if err != nil {
			return err
		}
	}
}

func (n *MuxerAdapter) AddStream(info media.StreamInfo) (int, error) {
	return n.engine.AddStream(info)
}

func (n *MuxerAdapter) SetMetadata(meta *metadata.Bundle) error {
	if meta == nil {
		return n.engine.SetMetadata(metadata.Bundle{})
	}
	return n.engine.SetMetadata(*meta)
}

func (n *MuxerAdapter) InputPorts() map[string]*node.InPort[*media.Packet] {
	return map[string]*node.InPort[*media.Packet]{
		"in": n.in,
	}
}
