package registry

import (
	"io"

	"github.com/godexture/core/node"
)

type Configration interface {
	NodeConfigaration()
}

type NodeFactory func(config Configration) (node.Node, error)

type MuxerFactory func(w io.Writer, config Configration) (node.Muxer, error)
type DemuxerFactory func(r io.Reader, config Configration) (node.Demuxer, error)

type EncoderFactory func(config Configration) (node.Encoder, error)
type DecoderFactory func(config Configration) (node.Decoder, error)

type FilterFactory func(config Configration) (node.Filter, error)
