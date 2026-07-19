package engine

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/core/node"
)

type DemuxerAdapter struct {
	engine    DemuxerEngine
	lifecycle engineLifecycle
	streams   []media.StreamInfo
	metadata  *metadata.Bundle
	analyzed  bool
	out       *node.OutPort[*media.Packet]
}

func WrapDemuxer(engine DemuxerEngine) node.Demuxer {
	adapter := &DemuxerAdapter{
		engine:    engine,
		lifecycle: newEngineLifecycle(engine),
		out:       node.NewOutPort[*media.Packet]("out", media.StreamInfo{}),
	}

	if seeker, ok := engine.(SeekerEngine); ok {
		return &SeekableDemuxerAdapter{
			DemuxerAdapter: adapter,
			seeker:         seeker,
		}
	}

	return adapter
}

func (n *DemuxerAdapter) Close() error {
	return n.lifecycle.Close()
}

type SeekableDemuxerAdapter struct {
	*DemuxerAdapter
	seeker SeekerEngine
}

func (n *SeekableDemuxerAdapter) Seek(offset time.Duration) error {
	return n.seeker.Seek(offset)
}

func (n *DemuxerAdapter) ensureAnalyzed() error {
	if n.analyzed {
		return nil
	}

	streams, meta, err := n.engine.Analyze()
	if err != nil {
		return err
	}
	n.streams = streams
	n.metadata = &meta
	n.analyzed = true
	return nil
}

func (n *DemuxerAdapter) Start(ctx context.Context) error {
	if err := n.ensureAnalyzed(); err != nil {
		return err
	}

	out := n.out.Edge()
	if out == nil {
		return fmt.Errorf("demuxer output not connected")
	}
	defer out.Close()

	for {
		pkt, _, err := n.engine.ReadPacket()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}

		if err := out.Push(ctx, pkt); err != nil {
			return err
		}
	}
}

func (n *DemuxerAdapter) Metadata() *metadata.Bundle {
	if n.metadata == nil {
		n.metadata = metadata.NewBundle()
	}
	return n.metadata
}

func (n *DemuxerAdapter) OutputPorts() map[string]*node.OutPort[*media.Packet] {
	info := media.StreamInfo{}
	if len(n.streams) > 0 {
		info = n.streams[0]
	}

	n.out.SetStreamInfo(info)
	return map[string]*node.OutPort[*media.Packet]{
		"out": n.out,
	}
}

func (n *DemuxerAdapter) Streams() ([]media.StreamInfo, error) {
	if err := n.ensureAnalyzed(); err != nil {
		return nil, err
	}
	return n.streams, nil
}
