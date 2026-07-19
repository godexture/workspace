package godec

import (
	"github.com/godexture/core/factory"
	"github.com/godexture/core/pipeline"
	"github.com/godexture/core/registry"
	"github.com/godexture/core/resolver"
)

var (
	DefaultMuxerRegistry   = registry.NewRegistry[registry.MuxerManifest]()
	DefaultDemuxerRegistry = registry.NewRegistry[registry.DemuxerManifest]()
	DefaultEncoderRegistry = registry.NewRegistry[registry.EncoderManifest]()
	DefaultDecoderRegistry = registry.NewRegistry[registry.DecoderManifest]()
	DefaultFilterRegistry  = registry.NewRegistry[registry.FilterManifest]()
)

var DefaultRegistry = registry.Bundle{
	Muxers:   DefaultMuxerRegistry,
	Demuxers: DefaultDemuxerRegistry,
	Encoders: DefaultEncoderRegistry,
	Decoders: DefaultDecoderRegistry,
	Filters:  DefaultFilterRegistry,
}

var f = factory.NewProvider(DefaultRegistry, resolver.Default)

var (
	NewRegistry   = f.NewRegistry
	NewResolver   = f.NewResolver
	NewNegotiator = f.NewNegotiator
	NewBuilder    = pipeline.NewBuilder
)

func Register(config registry.Configuration, manifest registry.Manifest) error {
	return DefaultRegistry.Register(config, manifest)
}
