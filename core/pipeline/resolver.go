package pipeline

import (
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
)

type ContainerResolver interface {
	ResolveDemuxer(r io.ReadSeeker) (registry.DemuxerFactory, error)
	ResolveMuxer(uri string) (registry.MuxerFactory, error)
}

type CodecResolver interface {
	ResolveDecoder(info media.StreamInfo) (registry.DecoderFactory, error)
	ResolveEncoder(profile media.Profile) (registry.EncoderFactory, error)
}

type PortResolver interface {
	ResolvePort(string) (string, error)
}

type ResolverBundle struct {
	container ContainerResolver
	codec     CodecResolver
	port      PortResolver
}
