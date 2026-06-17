package resolver

import (
	"io"

	"github.com/godexture/core/domain/media"
	"github.com/godexture/core/registry"
)

type MuxerResolver interface {
	ResolveMuxer(config registry.Configuration) (registry.MuxerManifest, error)
}

type DemuxerResolver interface {
	ResolveDemuxer(stream io.ReadSeeker, opts ...Option) (registry.DemuxerManifest, error)
}

type EncoderResolver interface {
	ResolveEncoder(codec media.CodecID, opts ...Option) (registry.EncoderManifest, error)
}

type DecoderResolver interface {
	ResolveDecoder(stream media.StreamInfo, opts ...Option) (registry.DecoderManifest, error)
}
