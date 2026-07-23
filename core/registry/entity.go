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
	Config Configuration
}

// Preparer is an optional node capability for resource-dependent setup. It is
// called after routing and linking, before the pipeline starts processing.
type Preparer interface {
	Prepare(ResourceGrant) error
}

type EncoderFactory func(media.StreamInfo, media.CodecID, TransformFactoryOptions) (node.Encoder, media.StreamInfo, error)
type DecoderFactory func(media.StreamInfo, TransformFactoryOptions) (node.Decoder, media.StreamInfo, error)

// FilterFactory builds a filter node from the resolved stream on each of its
// input ports, and reports the resolved stream on each of its output ports.
// Single-port filters normally use SingleFactory instead of implementing
// this directly.
type FilterFactory func(media.StreamSet, TransformFactoryOptions) (node.Filter, media.StreamSet, error)

// SingleFactory adapts a conventional single "in"->"out" filter factory to
// the general per-port FilterFactory signature.
func SingleFactory(factory func(media.StreamInfo, TransformFactoryOptions) (node.Filter, media.StreamInfo, error)) FilterFactory {
	return func(inputs media.StreamSet, options TransformFactoryOptions) (node.Filter, media.StreamSet, error) {
		filter, output, err := factory(inputs["in"], options)
		if err != nil {
			return nil, nil, err
		}
		return filter, media.StreamSet{"out": output}, nil
	}
}
