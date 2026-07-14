package registry

import (
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type Configuration interface {
	NodeConfiguration()
}

type NodeFactory func(Configuration) (node.Node, error)

type MuxerFactory func(io.Writer, Configuration) (node.Muxer, error)
type DemuxerFactory func(io.Reader, Configuration) (node.Demuxer, error)

type EncoderFactory func(media.StreamInfo, media.CodecID, Configuration) (node.Encoder, error)
type DecoderFactory func(media.StreamInfo, Configuration) (node.Decoder, error)

type FilterFactory func(media.StreamInfo, Configuration) (node.Filter, error)
