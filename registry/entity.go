package registry

import (
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/node"
)

type Configuration interface{}

type ConfigurationFactory func() Configuration

func NewConfigurationFactory[T any, Option any](newConfiguration func(...Option) T) ConfigurationFactory {
	return func() Configuration {
		configuration := newConfiguration()
		return &configuration
	}
}

type MuxerFactory func(io.Writer, Configuration) (node.Muxer, error)
type DemuxerFactory func(io.Reader, Configuration) (node.Demuxer, error)

type TransformFactoryOptions struct {
	Config    Configuration
	Resources ResourceBudget
}

type EncoderFactory func(media.StreamInfo, media.CodecID, TransformFactoryOptions) (node.Encoder, error)
type DecoderFactory func(media.StreamInfo, TransformFactoryOptions) (node.Decoder, error)

type FilterFactory func(media.StreamInfo, TransformFactoryOptions) (node.Filter, error)
