package registry

import (
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type Configuration interface {
	NodeConfiguration()
}

type NodeFactory func(config Configuration) (node.Node, error)

type MuxerFactory func(w io.Writer, config Configuration) (node.Muxer, error)
type DemuxerFactory func(r io.Reader, config Configuration) (node.Demuxer, error)

type EncoderFactory func(inStream media.StreamInfo, targetCodec media.CodecID, config Configuration) (node.Encoder, error)
type DecoderFactory func(stream media.StreamInfo, config Configuration) (node.Decoder, error)

type FilterFactory func(inStream media.StreamInfo, config Configuration) (node.Filter, error)

