package godec

import "github.com/godexture/core/registry"

var (
	DefaultRegistry = registry.Bundle{
		Demuxers: registry.NewRegistry[registry.DemuxerManifest](),
		Muxers:   registry.NewRegistry[registry.MuxerManifest](),
		Encoders: registry.NewRegistry[registry.EncoderManifest](),
		Decoders: registry.NewRegistry[registry.DecoderManifest](),
		Filters:  registry.NewRegistry[registry.FilterManifest](),
	}
)
