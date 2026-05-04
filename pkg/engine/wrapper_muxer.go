package engine

import (
	"context"
	"fmt"
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/domain/metadata"
	"github.com/godexture/core/domain/time"
	"github.com/godexture/core/node"
)

type MuxerAdapter struct {
	engine MuxerEngine
	in     *node.InPort[*media.Packet]
}

func WrapMuxer(engine MuxerEngine) node.Muxer {
	return &MuxerAdapter{
		engine: engine,
		in:     node.NewInPort[*media.Packet]("in", nil),
	}
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

		if err := n.engine.WritePacket(0, pkt); err != nil {
			return err
		}
	}
}

func (n *MuxerAdapter) AddStream(codecName string, _ time.Rational) (int, error) {
	codec := media.CodecID(codecName)
	if codec == "" {
		codec = media.CodecLPCM
	}

	format := media.SampleFormatS16
	layout := media.LayoutStereo2_0

	if codec != media.CodecLPCM {
		format = media.SampleFormatUnknown
		layout = media.NewUnspecified(0)
	}

	return n.engine.AddStream(media.StreamInfo{
		Index:     0,
		Type:      media.MediaAudio,
		IsDefault: true,
		MediaAttributes: media.MediaAttributes{
			Codec: codec,
			Audio: media.AudioAttributes{
				CodecID:       codec,
				SampleRate:    48000,
				Format:        format,
				ChannelLayout: layout,
			},
		},
	})
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
